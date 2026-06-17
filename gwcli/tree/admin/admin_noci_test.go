/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package admin_test

import (
	"encoding/csv"
	"slices"
	"strconv"
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
		u2Username = randomdata.SillyName()
		u2Password = randomdata.Month()
	)
	adminMeta := testsupport.MetaArgs(t, false, testsupport.WithDefaults())
	args := append(adminMeta, "admin", "users", "create",
		"--new-email="+randomdata.Email(),
		"--new-username="+u2Username,
		"--new-password="+u2Password,
		"--new-name="+randomdata.FirstName(0),
	)
	require.Zero(t, tree.Execute(args, &sbOut, &sbErr), sbErr.String())
	sbOut.Reset()
	sbErr.Reset()

	{ // create a bunch of data under a second user
		u2Meta := testsupport.MetaArgs(t, false, testsupport.WithServer(false, ""), testsupport.WithUsernamePassword(u2Username, u2Password))
		// saved query
		executeTree(t, &sbOut, &sbErr, u2Meta, "queries", "saved", "create", "--name="+u2Username+"_saved", "--query=\"tag=gravwell | limit 2\"")
		// dashboards
		// the TUI doesn't have the ability to create dashboards, so we are just skipping this for now.
		// kits
		// kit chowning isn't supported
		// extractors
		require.Zero(t, tree.Execute(append(adminMeta, "extractors", "find", "default"), &sbOut, &sbErr), sbErr.String()) // make sure we don't have a pre-existing extractor
		_, ID, found := strings.Cut(sbOut.String(), "ID: ")
		if found {
			ID, _, found = strings.Cut(ID, "\n")
			if found {
				require.Zero(t, tree.Execute(append(adminMeta, "extractors", "delete", ID), &sbOut, &sbErr), sbErr.String()) // kill the preexisting ax
			}
		}
		sbOut.Reset()
		sbErr.Reset()
		executeTree(t, &sbOut, &sbErr, u2Meta, "extractors", "create", "--name="+u2Username+"_extractor", "--module=csv", "--tags=default", "--params='babs'")
		// actionables
		executeTree(t, &sbOut, &sbErr, u2Meta, "actionables", "create", "--name="+u2Username+"_actionable")
		// playbooks
		executeTree(t, &sbOut, &sbErr, u2Meta, "playbook", "create", "--name="+u2Username+"_playbook_1")
		// scheduled searches
		executeTree(t, &sbOut, &sbErr, u2Meta, "queries", "scheduled", "create", "--name="+u2Username+"_saved",
			"--frequency=1 1 * * *",
			"--query=\"tag=gravwell | limit 2\"",
			"--duration", "1h",
		)
		// TODO add more asset types
	}

	var u2ID uint32
	{ // find second user's ID
		args := append(testsupport.MetaArgs(t, false, testsupport.WithDefaults()), "admin", "users", "list", "--csv", "--columns=ID,Username")
		require.Zero(t, tree.Execute(args, &sbOut, &sbErr), sbErr.String())
		out := sbOut.String()
		rdr := csv.NewReader(strings.NewReader(out))
		hdr, err := rdr.Read()
		require.Nil(t, err)
		require.True(t, slices.Equal(hdr, []string{"ID", "Username"}))
		records, err := rdr.ReadAll()
		require.Nil(t, err)
		for _, record := range records {
			if record[1] == u2Username {
				uid, err := strconv.ParseUint(record[0], 10, 32)
				require.Nil(t, err)
				u2ID = uint32(uid)
			}
		}
		sbOut.Reset()
		sbErr.Reset()
		require.NotZero(t, u2ID, "failed to read second user's ID from users list")
	}

	// take ownership of all of it
	args = append(testsupport.MetaArgs(t, false, testsupport.WithDefaults()), "admin", "mass-chown",
		"--to=1", "--from="+strconv.FormatUint(uint64(u2ID), 10))
	require.Zero(t, tree.Execute(args, &sbOut, &sbErr), sbErr.String())
	t.Log(sbOut.String())
	require.Empty(t, sbErr.String())
	sbOut.Reset()
	sbErr.Reset()
	// confirm we now own every item
	// saved query
	findName(t, &sbOut, &sbErr, u2Username+"_saved", adminMeta, []string{"queries", "saved"}, []string{"ID", "Name"})
	// dashboards
	// the TUI doesn't have the ability to create dashboards, so we are just skipping this for now.
	// kits
	// kit chowning isn't supported
	// extractors
	findName(t, &sbOut, &sbErr, u2Username+"_extractor", adminMeta, []string{"extractors"}, []string{"ID", "Name"})
	// actionables
	findName(t, &sbOut, &sbErr, u2Username+"_actionable", adminMeta, []string{"actionables"}, []string{"ID", "Name"})
	// playbooks
	findName(t, &sbOut, &sbErr, u2Username+"_playbook_1", adminMeta, []string{"playbooks"}, []string{"ID", "Name"})
	// scheduled searches
	/*	executeTree(t, &sbOut, &sbErr, adminMeta, "queries", "scheduled", "create", "--name="+u2Username+"_saved",
		"--frequency=1 1 * * *",
		"--query=\"tag=gravwell | limit 2\"",
		"--duration", "1h",
	)*/
}

// helper function.
// Calls tree.Execute, dies if a non-zero EC is returned, and resets the string builders.
func executeTree(t *testing.T, sbOut, sbErr *strings.Builder, meta []string, args ...string) {
	t.Helper()
	require.Zero(t, tree.Execute(append(meta, args...), sbOut, sbErr), sbErr.String())
	require.Empty(t, sbErr.String())
	sbOut.Reset()
	sbErr.Reset()
}

// automatically prefixes --csv and --columns to args
func findName(t *testing.T, sbOut, sbErr *strings.Builder, expectedName string, meta, parentNavPath, columns []string) {
	t.Helper()
	defer sbOut.Reset()
	defer sbErr.Reset()
	// compose args
	args := slices.Concat(meta, parentNavPath, []string{"list", "--csv", "--columns=" + strings.Join(columns, ",")})
	t.Log("findName args: ", args)
	require.Zero(t, tree.Execute(args, sbOut, sbErr), sbErr.String(), sbErr.String())
	require.Empty(t, sbErr.String())
	// check each column for the name
	rdr := csv.NewReader(strings.NewReader(sbOut.String()))
	t.Logf("parsing out:\n%v", sbOut.String())
	{ // check header length
		hdr, err := rdr.Read()
		require.Nil(t, err)
		assert.Len(t, hdr, len(hdr))
	}
	{ // find the name
		records, err := rdr.ReadAll()
		require.NoError(t, err)
		for _, record := range records {
			if slices.Contains(record, expectedName) {
				return
			}
		}
		t.Fatalf("failed to find item name %s in records %v", expectedName, records)
	}
}
