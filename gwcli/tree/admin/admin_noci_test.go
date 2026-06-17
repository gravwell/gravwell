/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package admin_test

import (
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogLevelGetSet(t *testing.T) {
	var curLevel string
	t.Run("get current log level", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"admin", "log-level",
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
		// parse out current log level
		_, after, found := strings.Cut(stdout.String(), "current log level: ")
		require.True(t, found)
		curLevel = strings.TrimSpace(after)
	})
	var setLevel = "info"
	t.Run("change log level", func(t *testing.T) {
		if setLevel == strings.ToLower(curLevel) { // don't want to set to the current level
			setLevel = "warn"
		}

		var stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"admin", "log-level", "--set="+setLevel,
			),
			nil,
			&stderr))
		require.Empty(t, stderr.String())
	})
	t.Run("get updated log level", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"admin", "log-level",
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
		// parse out current log level
		_, after, found := strings.Cut(stdout.String(), "current log level: ")
		require.True(t, found)
		curLevel = strings.ToLower(strings.TrimSpace(after))
		require.Equal(t, setLevel, curLevel)
	})
}

func TestMassChown(t *testing.T) {
	var sbOut, sbErr strings.Builder
	// create a second user
	var (
		u2Username string = randomdata.SillyName()
		u2Password string = randomdata.Month()
	)
	args := append(testsupport.MetaArgs(t, false, testsupport.WithDefaults()), "admin", "users", "create",
		"--new-email="+randomdata.Email(),
		"--new-username="+u2Username,
		"--new-password="+u2Password,
		"--new-name="+randomdata.FirstName(0),
	)
	require.Zero(t, tree.Execute(args, &sbOut, &sbErr), sbErr.String())
	sbOut.Reset()
	sbErr.Reset()

	// create a bunch of data under a second user
	u2Meta := testsupport.MetaArgs(t, false, testsupport.WithUsernamePassword(u2Username, u2Password))
	// saved query
	executeTree(t, &sbOut, &sbErr, u2Meta, "queries", "saved", "create", "--name="+u2Username+"saved", "--query=\"tag=gravwell | limit 2\"")
	// dashboards
	// the TUI doesn't have the ability to create dashboards, so we are just skipping this for now.
	// kits
	// kit chowning isn't supported
	// extractions

	executeTree(t, &sbOut, &sbErr, u2Meta, "queries", "saved", "create", "--name="+u2Username+"saved", "--query=\"tag=gravwell | limit 2\"")
	// take ownership of all of it
	// TODO
}

// helper function.
// Calls tree.Execute, dies if a non-zero EC is returned, and resets the string builders.
func executeTree(t *testing.T, sbOut, sbErr *strings.Builder, meta []string, args ...string) {
	t.Helper()
	require.Zero(t, tree.Execute(append(meta, args...), sbOut, sbErr), sbErr.String())
	sbOut.Reset()
	sbErr.Reset()
}
