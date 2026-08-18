package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/gosimple/slug"
	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
)

// GetClient returns an authenticated client for use by tests.
// This is not pooled, so try to limit to creating one per test.
func GetClient(t *testing.T) *client.Client {
	t.Helper()
	mtx.RLock()
	defer mtx.RUnlock()
	var server string
	if endpoint != nil && *endpoint != "" {
		server = *endpoint
		if *endpoint == "host.docker.internal" { // Docker Desktop specific, only resolves inside a container.
			server = "localhost"
		}
	} else {
		server, _ = instance.PortEndpoint(t.Context(), "80", "")
	}
	c, err := client.New(server, false, false)
	if err != nil {
		t.Fatalf("error creating client: %v", err)
	}
	err = c.Login("admin", "changeme")
	if err != nil {
		t.Fatalf("failed to login as admin: %v", err)
	}
	return c
}

// RunSearch will run a query over a time.Duration and return the entries and log them as an artifact.
// It will wait for the search to complete to simplify querying in a test.
func search(c *client.Client, query string, d time.Duration) (ents []types.StringTagEntry, s client.Search, err error) {
	if err = c.ParseSearch(query); err != nil {
		err = fmt.Errorf("failed to parse search query: %v", err)
		return
	}
	if s, err = c.StartSearch(query, time.Now().Add(-d), time.Now(), false); err != nil {
		err = fmt.Errorf("failed to start search: %v", err)
		return
	} else if err = c.WaitForSearch(s); err != nil {
		err = fmt.Errorf("failed to wait for search: %v", err)
		return
	}

	var cnt uint64
	var done bool
	if cnt, done, err = c.GetAvailableEntryCount(s); err != nil || !done {
		err = fmt.Errorf("error getting entry count: %v, count: %v, done: %v", err, cnt, done)
		return
	}
	if cnt == 0 {
		return
	}

	ents, err = c.GetEntries(s, 0, cnt)
	if err != nil {
		err = fmt.Errorf("failed to get entries: %v", err)
		return
	}

	return
}

// RunSearch will run a query over a time.Duration and return the entries and log them as an artifact.
// It will wait for the search to complete to simplify querying in a test.
func RunSearch(t *testing.T, c *client.Client, query string, d time.Duration) []types.StringTagEntry {
	t.Helper()
	ents, s, err := search(c, query, d)
	if err != nil {
		t.Fatalf("failed to search: %v", err)
	}
	if err = c.DeleteSearch(s.ID); err != nil {
		t.Logf("failed to delete search entry: %v", err)
	}

	WriteQueryResults(t, slug.Make(query), ents)

	return ents
}

func WaitForEntries(t *testing.T, c *client.Client, query string, d time.Duration, entries int, timeout time.Duration) []types.StringTagEntry {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lasterr error
	for time.Now().Before(deadline) {
		results, s, err := search(c, query, d)
		if err != nil {
			lasterr = err
		} else {
			if err = c.DeleteSearch(s.ID); err != nil {
				t.Logf("failed to delete search entry: %v", err)
			}
		}
		if len(results) >= entries {
			WriteQueryResults(t, slug.Make(query), results)
			return results
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("timed out after %s waiting for %v entries for search %q, error: %v", timeout.String(), entries, query, lasterr)
	return nil
}
