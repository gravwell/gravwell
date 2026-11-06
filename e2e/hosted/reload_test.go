package hosted

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
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
	runnerConfPath = "/opt/gravwell/etc/hosted_runner.conf"
	runnerLogPath  = "/opt/gravwell/log/hosted_runner.log"

	// log messages the runner emits while servicing a SIGHUP, these are the contract
	// the reload tests assert against
	reloadComplete    = "configuration reload complete"
	reloadFailed      = "failed to reload" // covers both the config load and the ingester swap
	ingesterAdded     = "created new ingester on reload"
	ingesterRemoved   = "shutdown ingester on reload"
	ingesterRestarted = "restarted ingester on reload"

	// settle is how long we let entries accumulate after a reload before searching, long
	// enough that a still running ingester on a 1s interval will certainly have written.
	settle = 15 * time.Second

	// shutdownGuard is skipped at the front of a post removal search window, see assertSilent
	shutdownGuard = 5 * time.Second
)

// tester and okta stanzas rendered into testdata/reload.conf
type testerPlugin struct {
	Name     string
	UUID     string
	Interval string
	Tag      string
}

type oktaPlugin struct {
	Name   string
	UUID   string
	Domain string
	Token  string
}

// reloadConfig is the template data for testdata/reload.conf. The embedded IngestConfig
// supplies the Global section the same way e2e.DefaultConfig does for the other tests.
type reloadConfig struct {
	config.IngestConfig
	Testers []testerPlugin
	Oktas   []oktaPlugin
}

func newReloadConfig(testers ...testerPlugin) reloadConfig {
	return reloadConfig{IngestConfig: e2e.DefaultConfig, Testers: testers}
}

// TestHostedReload covers the SIGHUP driven config reload. Every case runs several
// ingesters and only touches one of them, so each case also proves the runner left the
// bystanders alone rather than cycling everything on every reload.
func TestHostedReload(t *testing.T) {
	t.Run("Added Ingester", testReloadAdd)
	t.Run("Removed Ingester", testReloadRemove)
	t.Run("Changed Ingester", testReloadChange)
	t.Run("Renamed Ingester", testReloadRename)
	t.Run("Reused UUID", testReloadReusedUUID)
}

// a wholly new ingester shows up in the config and gets started, the existing two are untouched
func testReloadAdd(t *testing.T) {
	alpha := testerPlugin{"add-alpha", "1a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d001", "1s", "reload-add-alpha"}
	beta := testerPlugin{"add-beta", "1a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d002", "1s", "reload-add-beta"}
	gamma := testerPlugin{"add-gamma", "1a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d003", "1s", "reload-add-gamma"}

	con := startRunner(t, "hosted-reload-add", newReloadConfig(alpha, beta))
	c := e2e.GetClient(t)
	assertEmitting(t, c, time.Time{}, alpha.Tag, beta.Tag)

	at := reload(t, con, newReloadConfig(alpha, beta, gamma))

	assertAction(t, con, gamma.Name, ingesterAdded)
	assertUntouched(t, con, alpha.Name, beta.Name)

	waitOutWindow(t, at)
	assertEmitting(t, c, at, gamma.Tag, alpha.Tag, beta.Tag)
}

// an ingester disappears from the config, it gets shut down and stops writing while the rest keep going
func testReloadRemove(t *testing.T) {
	alpha := testerPlugin{"rm-alpha", "2a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d001", "1s", "reload-rm-alpha"}
	beta := testerPlugin{"rm-beta", "2a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d002", "1s", "reload-rm-beta"}
	gamma := testerPlugin{"rm-gamma", "2a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d003", "1s", "reload-rm-gamma"}

	con := startRunner(t, "hosted-reload-remove", newReloadConfig(alpha, beta, gamma))
	c := e2e.GetClient(t)
	assertEmitting(t, c, time.Time{}, alpha.Tag, beta.Tag, gamma.Tag)

	at := reload(t, con, newReloadConfig(alpha, beta))

	assertAction(t, con, gamma.Name, ingesterRemoved)
	assertUntouched(t, con, alpha.Name, beta.Name)

	waitOutWindow(t, at)
	assertSilent(t, c, at, gamma.Tag)
	assertEmitting(t, c, at, alpha.Tag, beta.Tag)
}

// a trivial but real config change (the poll interval) restarts just that one ingester
func testReloadChange(t *testing.T) {
	alpha := testerPlugin{"chg-alpha", "3a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d001", "1s", "reload-chg-alpha"}
	beta := testerPlugin{"chg-beta", "3a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d002", "1s", "reload-chg-beta"}
	gamma := testerPlugin{"chg-gamma", "3a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d003", "1s", "reload-chg-gamma"}

	con := startRunner(t, "hosted-reload-change", newReloadConfig(alpha, beta, gamma))
	c := e2e.GetClient(t)
	assertEmitting(t, c, time.Time{}, alpha.Tag, beta.Tag, gamma.Tag)

	// re-render the config with nothing changed at all, the runner must not cycle anything
	reload(t, con, newReloadConfig(alpha, beta, gamma))
	assertUntouched(t, con, alpha.Name, beta.Name, gamma.Name)

	changed := gamma
	changed.Interval = "2s"
	at := reload(t, con, newReloadConfig(alpha, beta, changed))

	assertAction(t, con, gamma.Name, ingesterRestarted)
	assertUntouched(t, con, alpha.Name, beta.Name)

	waitOutWindow(t, at)
	assertEmitting(t, c, at, gamma.Tag, alpha.Tag, beta.Tag)
}

// the ingester UUID stays put but the name changes, that alone is a complete change and
// the ingester is torn down and rebuilt under the new name
func testReloadRename(t *testing.T) {
	alpha := testerPlugin{"rn-alpha", "4a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d001", "1s", "reload-rn-alpha"}
	beta := testerPlugin{"rn-beta", "4a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d002", "1s", "reload-rn-beta"}
	gamma := testerPlugin{"rn-gamma", "4a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d003", "1s", "reload-rn-gamma"}

	con := startRunner(t, "hosted-reload-rename", newReloadConfig(alpha, beta, gamma))
	c := e2e.GetClient(t)
	assertEmitting(t, c, time.Time{}, alpha.Tag, beta.Tag, gamma.Tag)

	// only the stanza name moves, same UUID, same interval, same tag
	renamed := gamma
	renamed.Name = "rn-delta"
	at := reload(t, con, newReloadConfig(alpha, beta, renamed))

	assertAction(t, con, renamed.Name, ingesterRestarted)
	assertUntouched(t, con, gamma.Name, alpha.Name, beta.Name)

	waitOutWindow(t, at)
	assertEmitting(t, c, at, renamed.Tag, alpha.Tag, beta.Tag)
}

// an ingester UUID that belonged to one plugin is handed to a different plugin, the old
// plugin has to come down and the new one has to come up in its place
func testReloadReusedUUID(t *testing.T) {
	const sharedUUID = "5a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d003"
	alpha := testerPlugin{"uuid-alpha", "5a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d001", "1s", "reload-uuid-alpha"}
	beta := testerPlugin{"uuid-beta", "5a7a3e04-2e3c-4f6f-9f5a-2a1b6a19d002", "1s", "reload-uuid-beta"}
	// the okta plugin never reaches a real okta tenant, we only care that it holds the UUID
	swapped := oktaPlugin{"uuid-swap", sharedUUID, "e2e-reload.okta.com", "not-a-real-token"}
	tester := testerPlugin{swapped.Name, sharedUUID, "1s", "reload-uuid-swap"}

	initial := newReloadConfig(alpha, beta)
	initial.Oktas = []oktaPlugin{swapped}
	con := startRunner(t, "hosted-reload-reused-uuid", initial)
	c := e2e.GetClient(t)
	assertEmitting(t, c, time.Time{}, alpha.Tag, beta.Tag)

	// same name, same UUID, different plugin entirely
	at := reload(t, con, newReloadConfig(alpha, beta, tester))

	assertAction(t, con, tester.Name, ingesterRestarted)
	assertUntouched(t, con, alpha.Name, beta.Name)
	if line := findAction(t, con, tester.Name)[0]; !strings.Contains(line, `kind="tester"`) {
		e2e.Fatalf(t, "UUID %s was not handed to the tester plugin: %s", sharedUUID, line)
	}

	// the tester now owning the UUID is running, which it could not be if the okta
	// plugin still held the slot
	waitOutWindow(t, at)
	assertEmitting(t, c, at, tester.Tag, alpha.Tag, beta.Tag)
}

// startRunner brings up a hosted runner container with the given plugin set.
func startRunner(t *testing.T, name string, cfg reloadConfig) *tc.DockerContainer {
	t.Helper()
	con, err := tc.Run(t.Context(), "gravwell/hosted:e2e",
		e2e.WithDefaults(t, name,
			tc.WithWaitStrategyAndDeadline(
				35*time.Second,
				wait.ForLog("Successfully connected to ingesters").WithPollInterval(time.Second),
			),
			tc.WithFiles(tc.ContainerFile{
				Reader:            bytes.NewReader(renderConfig(t, "initial.conf", cfg)),
				ContainerFilePath: runnerConfPath,
				FileMode:          0o644,
			}),
		)...,
	)
	t.Cleanup(func() {
		e2e.SaveTestFiles(t, con, e2e.Log, []string{runnerLogPath})
		e2e.Terminate(t, con)
	})
	if err != nil {
		e2e.Fatal(t, err)
	}
	return con
}

// reload swaps the config out from under the running runner and SIGHUPs it, returning the
// instant the runner reported the reload finished. It blocks on that report so callers are
// never racing the signal handler when they go assert on what happened.
func reload(t *testing.T, con *tc.DockerContainer, cfg reloadConfig) time.Time {
	t.Helper()
	// the completion marker from any earlier reload is still sitting in the log, so count
	// what is already there and wait for one more rather than matching a stale line
	done := countLog(t, con, reloadComplete) + 1

	if err := con.CopyToContainer(t.Context(), renderConfig(t, "reloaded.conf", cfg), runnerConfPath, 0o644); err != nil {
		e2e.Fatalf(t, "failed to replace the running config: %v", err)
	}
	// the image starts the binary from a shell CMD, so signal it by name instead of assuming PID 1
	code, out, err := con.Exec(t.Context(), []string{"sh", "-c", "kill -HUP $(pidof runner)"})
	if err != nil {
		e2e.Fatalf(t, "failed to send SIGHUP: %v", err)
	} else if code != 0 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(out)
		e2e.Fatalf(t, "SIGHUP exited %d: %s", code, buf.String())
	}
	waitForReload(t, con, done, 30*time.Second)
	return time.Now()
}

// renderConfig renders testdata/reload.conf and saves the result as a test artifact.
func renderConfig(t *testing.T, artifact string, cfg reloadConfig) []byte {
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

// runnerLog pulls the current contents of the runner log out of the container.
func runnerLog(t *testing.T, con *tc.DockerContainer) []string {
	t.Helper()
	r, err := con.CopyFileFromContainer(t.Context(), runnerLogPath)
	if err != nil {
		e2e.Fatalf(t, "failed to read %s: %v", runnerLogPath, err)
	}
	defer r.Close()
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err = scanner.Err(); err != nil {
		e2e.Fatalf(t, "failed to scan %s: %v", runnerLogPath, err)
	}
	return lines
}

func countLog(t *testing.T, con *tc.DockerContainer, msg string) (n int) {
	t.Helper()
	for _, line := range runnerLog(t, con) {
		if strings.Contains(line, msg) {
			n++
		}
	}
	return
}

// waitForReload blocks until the runner has logged want reload completions, or fails the
// test. A reload that blew up never logs its completion, so surface that error instead of
// sitting here until the timeout with nothing useful to say.
func waitForReload(t *testing.T, con *tc.DockerContainer, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var done int
		for _, line := range runnerLog(t, con) {
			if strings.Contains(line, reloadComplete) {
				done++
			} else if strings.Contains(line, reloadFailed) {
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

// findAction returns every reload action the runner logged against the named ingester.
// Only reloads log these messages, startup does not, so anything here came from a SIGHUP.
func findAction(t *testing.T, con *tc.DockerContainer, name string) []string {
	t.Helper()
	var r []string
	for _, line := range runnerLog(t, con) {
		if !strings.Contains(line, `name="`+name+`"`) {
			continue
		}
		for _, action := range []string{ingesterAdded, ingesterRemoved, ingesterRestarted} {
			if strings.Contains(line, action) {
				r = append(r, line)
				break
			}
		}
	}
	return r
}

// assertAction requires that the named ingester saw exactly one reload action, and that it
// was the expected one. More than one means the reload cycled it more than it needed to.
func assertAction(t *testing.T, con *tc.DockerContainer, name, want string) {
	t.Helper()
	actions := findAction(t, con, name)
	if len(actions) == 0 {
		e2e.Fatalf(t, "ingester %q was not %q on reload", name, want)
	} else if len(actions) > 1 {
		e2e.Fatalf(t, "ingester %q saw %d reload actions, want 1: %s", name, len(actions), strings.Join(actions, " | "))
	} else if !strings.Contains(actions[0], want) {
		e2e.Fatalf(t, "ingester %q got the wrong reload action, want %q: %s", name, want, actions[0])
	}
}

// assertUntouched requires that the reload did not add, remove, or restart any of the named
// ingesters, they were not the target of the change and must have been left running.
func assertUntouched(t *testing.T, con *tc.DockerContainer, names ...string) {
	t.Helper()
	for _, name := range names {
		if actions := findAction(t, con, name); len(actions) > 0 {
			e2e.Fatalf(t, "ingester %q should not have been touched by the reload: %s", name, strings.Join(actions, " | "))
		}
	}
}

// searchWindow returns how far back to search. Anchoring on the reload instant means the
// range starts exactly when the reload finished, no matter how long the log assertions in
// between took. A fixed width window can slide back past the reload and turn entries the
// old ingester legitimately wrote before it was stopped into a failure. Pass the zero time
// before any reload has happened, when there is nothing to anchor to.
func searchWindow(since time.Time) time.Duration {
	if since.IsZero() {
		return settle
	}
	return time.Since(since)
}

// assertEmitting requires fresh entries on each tag, which only a running ingester produces.
func assertEmitting(t *testing.T, c *client.Client, since time.Time, tags ...string) {
	t.Helper()
	for _, tag := range tags {
		if ents := e2e.WaitForEntries(t, c, "tag="+tag, searchWindow(since), 1, 45*time.Second); len(ents) == 0 {
			e2e.Fatalf(t, "no entries on tag %s, its ingester is not running", tag)
		}
	}
}

// assertSilent requires that the tag stopped producing. The window starts a few seconds
// after the reload rather than at it: a tester on a one second interval can land a single
// entry right around the moment it is torn down, and that straggler says nothing about
// whether the ingester is still running. An ingester that really is still up writes once a
// second, so it cannot hide in the rest of the window. Call this only after waitOutWindow,
// so there is enough window left to be meaningful.
func assertSilent(t *testing.T, c *client.Client, since time.Time, tag string) {
	t.Helper()
	d := time.Since(since.Add(shutdownGuard))
	if d < time.Second {
		e2e.Fatalf(t, "assertSilent called %v after the reload, too soon to prove anything", time.Since(since))
	}
	if ents := e2e.RunSearch(t, c, "tag="+tag, d); len(ents) > 0 {
		stamps := make([]string, 0, len(ents))
		for _, ent := range ents {
			stamps = append(stamps, ent.TS.Format(time.RFC3339Nano))
		}
		e2e.Fatalf(t, "got %d entries on tag %s after its ingester was shut down at %s: %s",
			len(ents), tag, since.Format(time.RFC3339Nano), strings.Join(stamps, ", "))
	}
}

// waitOutWindow gives the reloaded config long enough to produce entries before anything
// searches for them, so an empty result means stopped rather than just not started yet.
func waitOutWindow(t *testing.T, since time.Time) {
	t.Helper()
	if d := settle - time.Since(since); d > 0 {
		time.Sleep(d)
	}
}
