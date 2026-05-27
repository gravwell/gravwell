//go:build noci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package groups_test

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestComplete executes a workflow designed to test every action in the `groups` nav.
//
// create -> list -> associate -> users -> disassociate -> users -> delete -> list
func TestComplete(t *testing.T) {
	var (
		groupName        = randomdata.SillyName()
		groupDescription = "created by " + t.Name()
		groupID          = 0 // set by the first test
	)

	t.Run("create a group", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"admin", "groups", "create", "--name="+groupName, "--description="+groupDescription,
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
		n, err := fmt.Sscanf(stdout.String(), "successfully created group (ID: %d)", &groupID)
		assert.Equal(t, 1, n)
		require.Nil(t, err)
	})
	if t.Failed() {
		t.FailNow()
	}
	t.Logf("group name: %s | group ID: %d", groupName, groupID)

	t.Run("find new group in list of groups", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"admin", "groups", "list", "--columns=ID,Name,Description", "--csv",
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
		rdr := csv.NewReader(strings.NewReader(stdout.String()))
		records, err := rdr.ReadAll()
		require.Nil(t, err, "failed to scan csv. Stdout: %s", stdout.String())
		require.Greater(t, len(records), 1)
		var found bool
		for _, row := range records[1:] {
			i, err := strconv.ParseInt(row[0], 10, 32)
			require.Nil(t, err)
			if i == int64(groupID) {
				found = true
				// sanity check the other values
				assert.Equal(t, groupName, row[1])
				assert.Equal(t, groupDescription, row[2])
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("associate admin user to the group", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"admin", "groups", "associate",
				"--gid="+strconv.FormatInt(int64(groupID), 10),
				"--uid=1",
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
	})
	t.Run("check that associate worked via `users`", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"admin", "groups", "users", strconv.FormatInt(int64(groupID), 10),
				"--json", "--columns=ID,Username",
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
		var m = []struct {
			ID       int    `json:"ID"`
			Username string `json:"Username"`
		}{}
		assert.Nil(t, json.Unmarshal([]byte(stdout.String()), &m), "stdout: %v", stdout.String())
		require.Len(t, m, 1) // we should have exactly one group member
		assert.Equal(t, 1, m[0].ID)
		assert.Equal(t, "admin", m[0].Username)
	})
	t.Run("disassociate", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"admin", "groups", "disassociate",
				"--gid="+strconv.FormatInt(int64(groupID), 10),
				"--uid=1",
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
	})
	t.Run("check that associate worked via `users`", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"admin", "groups", "users", strconv.FormatInt(int64(groupID), 10),
				"--json", "--columns=ID,Username",
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
		var m = []struct {
			ID       int    `json:"ID"`
			Username string `json:"Username"`
		}{}
		assert.Nil(t, json.Unmarshal([]byte(stdout.String()), &m), "stdout: %v", stdout.String())
		require.Len(t, m, 0) // we should not have any group members remaining
	})
	t.Run("delete the new group", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"admin", "groups", "delete",
				"--id="+strconv.FormatInt(int64(groupID), 10),
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
	})
	t.Run("ensure the group no longer exists", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"admin", "groups", "list", "--columns=ID,Name,Description", "--csv",
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
		rdr := csv.NewReader(strings.NewReader(stdout.String()))
		records, err := rdr.ReadAll()
		require.Nil(t, err, "failed to scan csv. Stdout: %s", stdout.String())
		var found bool
		for _, row := range records[1:] {
			i, err := strconv.ParseInt(row[0], 10, 32)
			require.Nil(t, err)
			if i == int64(groupID) {
				found = true
				break
			}
		}
		assert.False(t, found)
	})
}
