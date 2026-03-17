package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/gosimple/slug"
	"github.com/gravwell/gravwell/v3/client"
	"github.com/gravwell/gravwell/v3/client/types"
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
		t.Fatal(fmt.Errorf("error creating client: %w", err))
	}
	err = c.Login("admin", "changeme")
	if err != nil {
		t.Fatal(fmt.Errorf("failed to login as admin: %w", err))
	}
	return c
}

// RunSearch will run a query over a time.Duration and return the entries and log them as an artifact.
// It will wait for the search to complete to simplify querying in a test.
func RunSearch(t *testing.T, c *client.Client, query string, d time.Duration) []types.StringTagEntry {
	t.Helper()
	var err error
	if err = c.ParseSearch(query); err != nil {
		t.Fatal(fmt.Errorf("failed to parse search query: %w", err))
	}
	var s client.Search
	if s, err = c.StartSearch(query, time.Now().Add(-d), time.Now(), false); err != nil {
		t.Fatal(fmt.Errorf("failed to start search: %w", err))
	} else if err = c.WaitForSearch(s); err != nil {
		t.Fatal(fmt.Errorf("failed to wait for search: %w", err))
	}

	var cnt uint64
	var done bool
	if cnt, done, err = c.GetAvailableEntryCount(s); err != nil || !done {
		t.Fatal(fmt.Errorf("error getting entry count: %w, count: %v, done: %v", err, cnt, done))
	}
	if cnt == 0 {
		return []types.StringTagEntry{}
	}

	ent, err := c.GetEntries(s, 0, cnt)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to get entries: %w", err))
	}
	if err = c.DeleteSearch(s.ID); err != nil {
		t.Log(fmt.Errorf("failed to delete search entry: %w", err))
	}

	WriteQueryResults(t, slug.Make(query), ent)

	return ent
}
