//go:build noci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package email_test

import (
	"encoding/csv"
	"strconv"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmailConfigurationGetSet is a regression test for gravwell/issues#2424:
// deleting a user's email configuration used to leave the backend unable to
// service subsequent `email show`/`email configure` calls (400s caused by a
// stale, empty preference record). Delete -> show -> configure -> show must
// all succeed cleanly.
func TestEmailConfigurationGetSet(t *testing.T) {
	// destroy any pre-existing configuration
	t.Run("delete", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"email", "delete",
			),
			tree.ExecuteOptions{
				Stdout: &stdout,
				Stderr: &stderr,
			},
		))
		require.Empty(t, stderr.String())
	})
	if t.Failed() {
		t.FailNow()
	}
	t.Run("get empty configuration", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"email", "show",
			),
			tree.ExecuteOptions{
				Stdout: &stdout,
				Stderr: &stderr,
			},
		), stderr.String())
		require.Empty(t, stderr.String())
		require.Equal(t, "you do not have a mail server configured", strings.TrimSpace(strings.ToLower(stdout.String())))
	})
	if t.Failed() {
		t.FailNow()
	}
	emlServer := "server"
	emlPort := 666
	emlUsername := "user"
	// tls is also set
	t.Run("set configuration", func(t *testing.T) {
		var stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"email", "configure",
				"--email-server="+emlServer,
				"--email-port="+strconv.FormatInt(int64(emlPort), 10),
				"--email-username="+emlUsername,
				"--tls",
			),
			tree.ExecuteOptions{Stderr: &stderr},
		))
		require.Empty(t, stderr.String())
	})
	if t.Failed() {
		t.FailNow()
	}
	t.Run("get configuration", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"email", "show", "--csv", "--columns=Server,Port,Username,UseTLS,InsecureSkipVerify",
			),
			tree.ExecuteOptions{
				Stdout: &stdout,
				Stderr: &stderr,
			},
		))
		require.Empty(t, stderr.String())
		rdr := csv.NewReader(strings.NewReader(stdout.String()))
		hdr, err := rdr.Read()
		assert.Nil(t, err)
		require.Len(t, hdr, 5)
		row, err := rdr.Read()
		assert.Nil(t, err)
		require.Len(t, row, 5)
		assert.Equal(t, emlServer, row[0])
		assert.Equal(t, strconv.FormatInt(int64(emlPort), 10), row[1])
		assert.Equal(t, emlUsername, row[2])
		assert.Equal(t, strconv.FormatBool(true), row[3])
		assert.Equal(t, strconv.FormatBool(true), row[4])
	})

	// regression check for gravwell/issues#2424: delete the configuration we
	// just set, then confirm both `show` and `configure` still work cleanly
	// afterward, rather than returning a stale/broken preference error.
	t.Run("delete again", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"email", "delete",
			),
			tree.ExecuteOptions{
				Stdout: &stdout,
				Stderr: &stderr,
			},
		))
		require.Empty(t, stderr.String())
	})
	if t.Failed() {
		t.FailNow()
	}
	t.Run("show after second delete is empty, not an error", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"email", "show",
			),
			tree.ExecuteOptions{
				Stdout: &stdout,
				Stderr: &stderr,
			},
		), stderr.String())
		require.Empty(t, stderr.String())
		require.Equal(t, "you do not have a mail server configured", strings.TrimSpace(strings.ToLower(stdout.String())))
	})
	if t.Failed() {
		t.FailNow()
	}
	t.Run("re-configure after delete", func(t *testing.T) {
		var stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"email", "configure",
				"--email-server="+emlServer,
				"--email-port="+strconv.FormatInt(int64(emlPort), 10),
				"--email-username="+emlUsername,
				"--tls",
			),
			tree.ExecuteOptions{Stderr: &stderr},
		))
		require.Empty(t, stderr.String())
	})
	if t.Failed() {
		t.FailNow()
	}
	t.Run("get re-configured configuration", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"email", "show", "--csv", "--columns=Server,Port,Username,UseTLS,InsecureSkipVerify",
			),
			tree.ExecuteOptions{
				Stdout: &stdout,
				Stderr: &stderr,
			},
		))
		require.Empty(t, stderr.String())
		rdr := csv.NewReader(strings.NewReader(stdout.String()))
		hdr, err := rdr.Read()
		assert.Nil(t, err)
		require.Len(t, hdr, 5)
		row, err := rdr.Read()
		assert.Nil(t, err)
		require.Len(t, row, 5)
		assert.Equal(t, emlServer, row[0])
		assert.Equal(t, strconv.FormatInt(int64(emlPort), 10), row[1])
		assert.Equal(t, emlUsername, row[2])
		assert.Equal(t, strconv.FormatBool(true), row[3])
		assert.Equal(t, strconv.FormatBool(true), row[4])
	})
}
