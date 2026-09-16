package HttpIngester

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"text/template"
	"time"

	"gravwell/e2e"

	"github.com/gravwell/gravwell/v3/client"
	"github.com/gravwell/gravwell/v3/ingest/config"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	reloadConfPath = "/opt/gravwell/etc/gravwell_http_ingester.conf"
	reloadLogPath  = "/opt/gravwell/log/http_ingester.log"

	// the ingester logs this once it has accepted a new config off a SIGHUP
	reloadComplete = "loaded new config"
	// ...and these when it did not, surface them instead of burning the timeout
	reloadParseFailed = "failed to parse new configuration"
	reloadLoadFailed  = "failed to load new configuration"
)

// reloadConfig is the template data for testdata/reload.conf, the embedded IngestConfig
// supplies the Global section the same way e2e.DefaultConfig does everywhere else.
type reloadConfig struct {
	config.IngestConfig
	Tag string
}

// TestHotReloadNegotiatesTag covers issue 2457.  A tag that shows up in a hot
// reloaded config has to be negotiated with the indexer right then, not lazily on
// the first entry that happens to carry it.  An ingester can sit on a freshly
// configured listener for hours before anything posts to it, and until the tag is
// established it cannot be searched, so this test never sends a single entry.
func TestHotReloadNegotiatesTag(t *testing.T) {
	before := uniqueTag(t, "reload-before")
	after := uniqueTag(t, "reload-after")

	con := startReloadIngester(t, reloadConfig{IngestConfig: e2e.DefaultConfig, Tag: before})
	c := e2e.GetClient(t)

	// the original tag rode in on the authentication handshake, if that is not true
	// the rest of the test proves nothing
	if !waitForTag(t, c, before, 30*time.Second) {
		e2e.Fatalf(t, "tag %q was never established from the initial config", before)
	}
	if hasTag(t, c, after) {
		e2e.Fatalf(t, "tag %q already exists before the reload, pick a different name", after)
	}

	reloadIngester(t, con, reloadConfig{IngestConfig: e2e.DefaultConfig, Tag: after})

	// NOTHING has been posted to /ingest at this point and nothing will be, the tag
	// must exist purely because the reload negotiated it
	if !waitForTag(t, c, after, 30*time.Second) {
		e2e.Fatalf(t, "tag %q was not negotiated with the indexer during the hot reload, no data has flowed on it", after)
	}

	// and it has to be searchable, which is what a user actually notices
	query := "tag=" + after
	if err := c.ParseSearch(query); err != nil {
		e2e.Fatalf(t, "search %q was rejected after the reload: %v", query, err)
	}
	if ents := e2e.RunSearch(t, c, query, time.Minute); len(ents) != 0 {
		e2e.Fatalf(t, "expected no entries on %q, got %d, something wrote to the tag", after, len(ents))
	}
}

// startReloadIngester brings up an HTTP ingester carrying a single listener on cfg.Tag.
func startReloadIngester(t *testing.T, cfg reloadConfig) *tc.DockerContainer {
	t.Helper()
	con, err := tc.Run(t.Context(), "",
		e2e.Ingester(t, "http-reload", "HttpIngester",
			tc.WithFiles(tc.ContainerFile{
				Reader:            bytes.NewReader(renderReloadConfig(t, "initial.conf", cfg)),
				ContainerFilePath: reloadConfPath,
				FileMode:          0o644,
			}),
			tc.WithExposedPorts("80/tcp"),
			tc.WithAdditionalWaitStrategyAndDeadline(10*time.Second,
				wait.NewHTTPStrategy("/health/check").WithPollInterval(time.Second)),
		)...,
	)
	t.Cleanup(func() {
		e2e.SaveTestFiles(t, con, e2e.Log, []string{reloadLogPath})
		e2e.Terminate(t, con)
	})
	if err != nil {
		e2e.Fatal(t, err)
	}
	return con
}

// reloadIngester swaps the config out from under the running ingester and SIGHUPs it,
// blocking until the ingester reports the reload finished so callers are never racing
// the signal handler.
func reloadIngester(t *testing.T, con *tc.DockerContainer, cfg reloadConfig) {
	t.Helper()
	// any earlier reload marker is still sitting in the log, count what is already
	// there and wait for one more rather than matching a stale line
	want := countLog(t, con, reloadComplete) + 1

	if err := con.CopyToContainer(t.Context(), renderReloadConfig(t, "reloaded.conf", cfg), reloadConfPath, 0o644); err != nil {
		e2e.Fatalf(t, "failed to replace the running config: %v", err)
	}
	// the image starts the binary from a shell CMD, so signal it by name instead of
	// assuming PID 1
	code, out, err := con.Exec(t.Context(), []string{"sh", "-c", "kill -HUP $(pidof HttpIngester)"})
	if err != nil {
		e2e.Fatalf(t, "failed to send SIGHUP: %v", err)
	} else if code != 0 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(out)
		e2e.Fatalf(t, "SIGHUP exited %d: %s", code, buf.String())
	}
	waitForReload(t, con, want, 30*time.Second)
}

// waitForReload blocks until the ingester has logged want reload completions.  A reload
// that blew up never logs its completion, so surface that error rather than sitting here
// until the timeout with nothing useful to say.
func waitForReload(t *testing.T, con *tc.DockerContainer, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var done int
		for _, line := range ingesterLog(t, con) {
			if strings.Contains(line, reloadComplete) {
				done++
			} else if strings.Contains(line, reloadParseFailed) || strings.Contains(line, reloadLoadFailed) {
				e2e.Fatalf(t, "reload failed: %s", line)
			}
		}
		if done >= want {
			return
		}
		if time.Now().After(deadline) {
			e2e.Fatalf(t, "timed out after %v waiting for reload %d to complete", timeout, want)
		}
		time.Sleep(time.Second)
	}
}

// ingesterLog pulls the current contents of the ingester log out of the container.
func ingesterLog(t *testing.T, con *tc.DockerContainer) []string {
	t.Helper()
	r, err := con.CopyFileFromContainer(t.Context(), reloadLogPath)
	if err != nil {
		e2e.Fatalf(t, "failed to read %s: %v", reloadLogPath, err)
	}
	defer r.Close()
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err = scanner.Err(); err != nil {
		e2e.Fatalf(t, "failed to scan %s: %v", reloadLogPath, err)
	}
	return lines
}

func countLog(t *testing.T, con *tc.DockerContainer, msg string) (n int) {
	t.Helper()
	for _, line := range ingesterLog(t, con) {
		if strings.Contains(line, msg) {
			n++
		}
	}
	return
}

// hasTag asks the Gravwell instance whether it knows about a tag.  This is the indexer
// side view, not the ingester's, which is the whole point of the exercise.
func hasTag(t *testing.T, c *client.Client, tag string) bool {
	t.Helper()
	tags, err := c.GetTags()
	if err != nil {
		e2e.Fatalf(t, "failed to list tags: %v", err)
	}
	return slices.Contains(tags, tag)
}

func waitForTag(t *testing.T, c *client.Client, tag string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if hasTag(t, c, tag) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Second)
	}
}

// uniqueTag keeps each run from colliding with tags an earlier run already established,
// a stale tag would make the assertions pass for the wrong reason.
func uniqueTag(t *testing.T, prefix string) string {
	t.Helper()
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("failed to generate a unique tag: %v", err)
	}
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b[:]))
}

// renderReloadConfig renders testdata/reload.conf and saves the result under its own
// artifact name, the initial and the reloaded config both want to be kept.
func renderReloadConfig(t *testing.T, artifact string, cfg reloadConfig) []byte {
	t.Helper()
	path := filepath.Clean("testdata/reload.conf")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read config template %s: %v", path, err)
	}
	tmpl, err := template.New(filepath.Base(path)).Parse(string(body))
	if err != nil {
		t.Fatalf("failed to parse config template %s: %v", path, err)
	}
	var buf bytes.Buffer
	if err = tmpl.Execute(&buf, cfg); err != nil {
		t.Fatalf("failed to render config template %s: %v", path, err)
	}
	e2e.WriteArtifact(t, e2e.Conf, artifact, buf.Bytes())
	return buf.Bytes()
}
