/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package okta

import (
	"context"
	"os"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// TestLiveOAuth runs the OAuth path against a real org (ideally a preview sandbox).
//
// It reads one users window and one system log page.
//
// Set OKTA_LIVE_DOMAIN, OKTA_LIVE_CLIENT_ID and OKTA_LIVE_PRIVATE_KEY_FILE to run it, OKTA_LIVE_KEY_ID is optional.
func TestLiveOAuth(t *testing.T) {
	domain, clientID, keyFile := os.Getenv(`OKTA_LIVE_DOMAIN`), os.Getenv(`OKTA_LIVE_CLIENT_ID`), os.Getenv(`OKTA_LIVE_PRIVATE_KEY_FILE`)
	if domain == `` || clientID == `` || keyFile == `` {
		t.Skip("set OKTA_LIVE_DOMAIN, OKTA_LIVE_CLIENT_ID and OKTA_LIVE_PRIVATE_KEY_FILE to run against a live org")
	}
	key, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("failed to read the private key file: %v", err)
	}

	rt := newTestRuntime(t)
	t.Cleanup(func() {
		for _, l := range rt.logged(``) {
			t.Log(l)
		}
	})
	o, err := NewOktaIngester(Config{
		Domain:         domain,
		Client_ID:      clientID,
		Private_Key:    string(key),
		Private_Key_ID: os.Getenv(`OKTA_LIVE_KEY_ID`),
	}, rt)
	if err != nil {
		t.Fatal(err)
	}
	o.c = o.newOAuthDoer(t.Context(), rate.NewLimiter(rate.Every(time.Second), 5), rt)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	end := time.Now().Add(-userLogWindowLag).Round(time.Second)
	if err = o.getUserLogs(ctx, end.Add(-24*time.Hour), end, rt); err != nil {
		t.Fatalf("users: %v", err)
	}
	users := len(rt.data(o.userTag))

	// stop once the first page has been ingested and its cursor saved
	o.latestTS = time.Now().Add(-24 * time.Hour)
	rt.onPut = func(key string) {
		if key == urlKey {
			rt.cancel()
		}
	}
	if err = o.getSystemLogs(rt.ctx, rt); err != nil {
		t.Fatalf("system logs: %v", err)
	}
	events := len(rt.data(o.systemTag))
	t.Logf("ingested %d system log events and %d users", events, users)
	if events+users == 0 {
		t.Error("no entries were written")
	}
	if _, serr := rt.Status(); serr != nil {
		t.Errorf("error status set: %v", serr)
	}
}
