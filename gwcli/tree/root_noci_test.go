//go:build noci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package tree

import (
	"errors"
	"os"
	"path"
	"testing"

	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/cobra"
)

const (
	enforceLoginDefaultUser string = "admin"
)

var enforceLoginDefaultPass = "changeme"

// buildEnforceLoginCmd constructs a bare cobra.Command carrying the same persistent flags
// the real root command has, parses args into it, and returns it ready to pass to
// EnforceLogin.
func buildEnforceLoginCmd(t *testing.T, args []string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "gwcli"}
	uniques.AttachPersistentFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	return cmd
}

// TestEnforceLogin_explicitCredsDoNotRetry confirms that when a user explicitly supplies bad
// credentials (u/p or an API token), EnforceLogin fails once with the underlying credential
// error instead of retrying/looping with the same, doomed-to-fail credentials.
//
// This exercises the `!explicitCreds` scoping added alongside the ErrNotAuthed failsafe:
// explicit credential failures are never caused by a stale local cache, so retrying with the
// same bad creds would be pointless.
func TestEnforceLogin_explicitCredsDoNotRetry(t *testing.T) {
	server := testsupport.Server()
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "DEBUG"); err != nil {
		t.Fatal(err)
	}

	t.Run("bad username/password", func(t *testing.T) {
		connection.Client = nil // force a fresh connection singleton
		t.Cleanup(func() { connection.End() })

		pfPath := path.Join(t.TempDir(), "pass.txt")
		if err := os.WriteFile(pfPath, []byte("not-the-real-password"), 0600); err != nil {
			t.Fatal(err)
		}

		cmd := buildEnforceLoginCmd(t, []string{
			"--server=" + server, "--insecure", "--" + ft.NoInteractive.Name(),
			"-u=" + enforceLoginDefaultUser, "--passfile=" + pfPath,
		})

		err := EnforceLogin(cmd, nil)
		if err == nil {
			t.Fatal("expected EnforceLogin to fail with bad credentials")
		}
		if !errors.Is(err, connection.ErrInvalidCredentials) {
			t.Fatalf("expected ErrInvalidCredentials, got: %v", err)
		}
	})

	t.Run("bad API token", func(t *testing.T) {
		connection.Client = nil
		t.Cleanup(func() { connection.End() })

		cmd := buildEnforceLoginCmd(t, []string{
			"--server=" + server, "--insecure", "--" + ft.NoInteractive.Name(),
			"--api=not-a-real-token",
		})

		err := EnforceLogin(cmd, nil)
		if err == nil {
			t.Fatal("expected EnforceLogin to fail with a bad API token")
		}
		if !errors.Is(err, connection.ErrAPITokenInvalid) {
			t.Fatalf("expected ErrAPITokenInvalid, got: %v", err)
		}
	})
}

// TestEnforceLogin_staleTokenAfterExternalLogout exercises the ErrNotAuthed failsafe path
// described in the bug report: a token file is left behind that references a session which
// has since been invalidated server-side *without* going through gwcli's own logout command
// (which would have deleted the file itself).
//
// Depending on exactly where the server rejects the stale session, EnforceLogin may either
// recover on its own or terminate cleanly with ErrNonInteractiveRequiresDifferentLogin -- but
// in every case it must never hang, panic, or resurface the raw, unwrapped
// "failed to cache user info: Not Authed" error that originally broke errors.Is() detection
// and left users permanently stuck.
func TestEnforceLogin_staleTokenAfterExternalLogout(t *testing.T) {
	server := testsupport.Server()
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "DEBUG"); err != nil {
		t.Fatal(err)
	}

	// isolate the token file
	orig := cfgdir.DefaultTokenPath
	cfgdir.DefaultTokenPath = path.Join(t.TempDir(), "tknfile")
	t.Cleanup(func() { cfgdir.DefaultTokenPath = orig })

	// log in normally via gwcli's own connection package to produce a real token file
	if err := connection.Initialize(server, false, true, path.Join(t.TempDir(), "rest.log")); err != nil {
		t.Fatal(err)
	}
	if err := connection.Login(enforceLoginDefaultUser, &enforceLoginDefaultPass, nil, true, nil, nil); err != nil {
		t.Fatal("initial login failed: ", err)
	}

	// invalidate the session server-side *without* going through gwcli's logout command
	// (connection.End() intentionally never deletes the token file -- see its doc comment),
	// leaving a stale, on-disk token exactly as described in the original bug report.
	if err := connection.Client.Logout(); err != nil {
		t.Fatal("failed to invalidate session server-side: ", err)
	}
	connection.End()

	if _, err := os.Stat(cfgdir.DefaultTokenPath); err != nil {
		t.Fatalf("expected a stale token file to still be present before retrying login: %v", err)
	}
	t.Cleanup(func() { connection.End() })

	cmd := buildEnforceLoginCmd(t, []string{
		"--server=" + server, "--insecure", "--" + ft.NoInteractive.Name(),
	})

	err := EnforceLogin(cmd, nil)

	switch {
	case err == nil:
		// the failsafe fully recovered; acceptable.
	case errors.Is(err, client.ErrNotAuthed):
		// the failsafe detected the stale session via the ErrNotAuthed branch; per
		// root.go it must have cleared the token file before giving up.
		if _, statErr := os.Stat(cfgdir.DefaultTokenPath); !os.IsNotExist(statErr) {
			t.Errorf("expected stale token file to be removed after an ErrNotAuthed failsafe retry, stat returned: %v", statErr)
		}
	case errors.Is(err, connection.ErrNonInteractiveRequiresDifferentLogin):
		// loginViaJWT rejected the stale token before ever reaching the ErrNotAuthed
		// failsafe branch (no credentials were supplied, so there is nothing else to
		// fall back on in script mode); this is a clean, expected outcome and, notably,
		// the local cache was left in whatever state the JWT check found it.
	default:
		t.Fatalf("unexpected error from EnforceLogin with a stale token: %v", err)
	}
}
