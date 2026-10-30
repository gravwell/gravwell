/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package logout_test

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/tree/logout"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
)

// TestNewAction_deletesCachedToken is a regression test confirming that running the logout
// action always removes the locally cached login token file, whether or not one was present
// and whether or not the client was actually authenticated at the time.
//
// Neither Client.Logout() nor connection.End() require a live server when the client has
// never actually authenticated (both are local, synchronous state checks in that case), so
// this test does not need a live Gravwell instance.
func TestNewAction_deletesCachedToken(t *testing.T) {
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "DEBUG"); err != nil {
		t.Fatal(err)
	}

	// isolate the token file
	orig := cfgdir.DefaultTokenPath
	t.Cleanup(func() { cfgdir.DefaultTokenPath = orig })

	tests := []struct {
		name            string
		tokenFileExists bool
	}{
		{"token file exists", true},
		{"token file already absent", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgdir.DefaultTokenPath = path.Join(t.TempDir(), "tknfile")
			if tt.tokenFileExists {
				if err := os.WriteFile(cfgdir.DefaultTokenPath, []byte("someuser\nsometoken"), 0600); err != nil {
					t.Fatal(err)
				}
			}

			// spin up a Client that has never actually authenticated against a server.
			if err := connection.Initialize("127.0.0.1:0", false, true, path.Join(t.TempDir(), "rest.log")); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { connection.End() })

			ap := logout.NewAction()

			if err := ap.Action.RunE(ap.Action, nil); err != nil {
				t.Fatal("logout action returned an error:", err)
			}

			if _, err := os.Stat(cfgdir.DefaultTokenPath); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("expected cached token file to be removed after logout, stat returned: %v", err)
			}
		})
	}
}
