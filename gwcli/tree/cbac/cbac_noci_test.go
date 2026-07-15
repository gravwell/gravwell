//go:build noci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package cbac_test

import (
	"encoding/csv"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// we can only run these tests if cbac is enabled
	cli, err := client.New(testsupport.Server(), false, false)
	if err != nil {
		panic(err)
	}
	if err := cli.Login("admin", "changeme"); err != nil {
		panic(err)
	}
	li, err := cli.GetLicenseInfo()
	cli.Close()
	// we don't actually have a way to check if cbac is enabled *in the system*, rather than just the license
	if err == nil && li.CBACEnabled() {
		m.Run()
	}
}

func TestCBACLifecycle(t *testing.T) {
	// spool up a client to check directly against the results we get
	cli, err := client.New(testsupport.Server(), false, false)
	if err != nil {
		t.Fatal(err)
	}
	require.Nil(t, cli.Login("admin", "changeme"))

	allCaps, err := cli.CapabilityList()
	if err != nil {
		t.Log("failed to get direct list of capabilities, skipping")
		t.Skip()
	}
	// sort the caps into a map for faster lookup
	allCapsMap := map[string]any{} // canonical name -> nothing
	for _, cap := range allCaps {
		allCapsMap[cap.Name] = 0
	}

	metaAdmin := testsupport.MetaArgs(t, false, testsupport.WithDefaults())

	t.Run("capabilities matches full list of caps and vice versa", func(t *testing.T) {
		var stdout, stderr strings.Builder
		capsPath := path.Join(t.TempDir(), "caps_list.csv")
		require.Zero(t, tree.Execute(append(metaAdmin, "cbac", "capabilities", "--csv", "-o", capsPath, "--columns=Name"), tree.ExecuteOptions{
			Stdout: &stdout,
			Stderr: &stderr,
		}), stderr.String())
		assert.Empty(t, stderr.String())
		f, err := os.Open(capsPath)
		require.Nil(t, err)
		defer f.Close()
		rdr := csv.NewReader(f)
		records, err := rdr.ReadAll()
		require.Nil(t, err)
		assert.Greater(t, len(records), 2)
		seenCaps := map[string]any{} // should make allCapsMap by the time we are done
		for i := 1; i < len(records); i++ {
			record := records[i]
			assert.Len(t, record, 1)
			seenCaps[record[0]] = 0
		}
		assert.True(t, maps.Equal(allCapsMap, seenCaps))
	})

	// spool up a second user that isn't an admin to test against
	secondUser, err := cli.CreateUser(
		types.AddUser{Username: randomdata.Alphanumeric(10), Password: "pass", Name: "ziv", Email: "ziv@example.com", Admin: false},
	)
	if err != nil {
		t.Logf("failed to create secondary user to test against: %v", err)
		t.Skip()
	}
	t.Cleanup(func() { cli.DeleteUser(secondUser.ID) })
	secondCli, err := client.New(testsupport.Server(), false, false)
	if err != nil {
		t.Logf("failed to create secondary user client: %v", err)
		t.Skip()
	}
	require.Nil(t, secondCli.Login(secondUser.Username, "pass"))
	metaSecond := testsupport.MetaArgs(t, false,
		testsupport.WithUsernamePassword(secondUser.Username, "pass"),
		testsupport.WithServer(false, ""),
	)
	t.Run("new user has no caps", func(t *testing.T) {
		var stdout, stderr strings.Builder
		require.Zero(t,
			tree.Execute(append(metaSecond, "cbac", "my", "--columns=Name"), tree.ExecuteOptions{
				Stdout: &stdout,
				Stderr: &stderr,
			}),
		)
		rdr := csv.NewReader(strings.NewReader(stdout.String()))
		header, err := rdr.Read()
		assert.Nil(t, err)
		assert.Len(t, header, 1)
		records, err := rdr.ReadAll()
		assert.Nil(t, err)
		require.Len(t, records, 0)

		stdout.Reset()
		stderr.Reset()

		// check that getting as admin agrees
		require.Zero(t,
			tree.Execute(append(metaAdmin, "cbac", "get", "--csv", "--columns=ID,Grants",
				"--uids="+strconv.FormatInt(int64(secondUser.ID), 10)),
				tree.ExecuteOptions{
					Stdout: &stdout,
					Stderr: &stderr,
				}),
			stderr.String())
		assert.Empty(t, stderr.String())
		rdr = csv.NewReader(strings.NewReader(stdout.String()))
		header, err = rdr.Read()
		assert.Nil(t, err)
		assert.Len(t, header, 2)
		records, err = rdr.ReadAll()
		assert.Nil(t, err)
		require.Len(t, records, 1)
		var uid int32
		fmt.Sscanf(records[0][0], "uid%d", &uid)
		assert.Equal(t, secondUser.ID, uid)
	})
	t.Run("edit with --revoke will add no caps", func(t *testing.T) {
		var stdout, stderr strings.Builder

		args := append(metaAdmin, "cbac", "edit", "--revoke", "--uid="+strconv.FormatInt(int64(secondUser.ID), 10))
		bareArgs := slices.Collect(maps.Keys(allCapsMap))
		require.Zero(t,
			tree.Execute(append(args, bareArgs...),
				tree.ExecuteOptions{
					Stdout: &stdout,
					Stderr: &stderr,
				}),
			stderr.String())
		userHasCaps(t, cli, secondUser.ID, []string{})
	})
	t.Run("replace to fill caps", func(t *testing.T) {
		var stdout, stderr strings.Builder

		expectedCaps := []string{"PivotRead", "KitWrite"}

		args := append(metaAdmin, "cbac", "set", "--uids="+strconv.FormatInt(int64(secondUser.ID), 10), "--caps="+strings.Join(expectedCaps, ","))
		bareArgs := slices.Collect(maps.Keys(allCapsMap))
		require.Zero(t,
			tree.Execute(append(args, bareArgs...),
				tree.ExecuteOptions{
					Stdout: &stdout,
					Stderr: &stderr,
				}),
			stderr.String())
		userHasCaps(t, cli, secondUser.ID, expectedCaps)
	})
	t.Run("replace supplants caps", func(t *testing.T) {
		var stdout, stderr strings.Builder

		expectedCaps := []string{"NotificationRead"}

		args := append(metaAdmin, "cbac", "set", "--uids="+strconv.FormatInt(int64(secondUser.ID), 10), "--caps="+strings.Join(expectedCaps, ","))
		require.Zero(t,
			tree.Execute(args, tree.ExecuteOptions{
				Stdout: &stdout,
				Stderr: &stderr,
			}),
			stderr.String())
		userHasCaps(t, cli, secondUser.ID, expectedCaps)
	})
	t.Run("edit without --grant or --revoke works like replace", func(t *testing.T) {
		var stdout, stderr strings.Builder

		expectedCaps := []string{"SaveSearch", "LicenseRead", "ListUsers"}

		args := append(metaAdmin, "cbac", "edit", "--uid="+strconv.FormatInt(int64(secondUser.ID), 10))
		require.Zero(t,
			tree.Execute(append(args, expectedCaps...),
				tree.ExecuteOptions{
					Stdout: &stdout,
					Stderr: &stderr,
				}),
			stderr.String())
		userHasCaps(t, cli, secondUser.ID, expectedCaps)
	})
	t.Run("edit with --grant adds only new caps", func(t *testing.T) {
		var stdout, stderr strings.Builder

		expectedCaps := []string{"SaveSearch", "LicenseRead", "ListUsers",
			"BackgroundSearch", // new
		}

		givenCaps := []string{"LicenseRead", // dup
			"ListUsers",        // dup
			"BackgroundSearch", // new
		}

		args := append(metaAdmin, "cbac", "edit", "--grant", "--uid="+strconv.FormatInt(int64(secondUser.ID), 10))
		require.Zero(t,
			tree.Execute(append(args, givenCaps...),
				tree.ExecuteOptions{
					Stdout: &stdout,
					Stderr: &stderr,
				}),
			stderr.String())
		userHasCaps(t, cli, secondUser.ID, expectedCaps)
	})
	t.Run("replace with no caps clears all", func(t *testing.T) {
		var stdout, stderr strings.Builder

		args := append(metaAdmin, "cbac", "set", "--uids="+strconv.FormatInt(int64(secondUser.ID), 10))
		require.Zero(t,
			tree.Execute(args, tree.ExecuteOptions{
				Stdout: &stdout,
				Stderr: &stderr,
			}),
			stderr.String())
		userHasCaps(t, cli, secondUser.ID, []string{})
	})
}

func userHasCaps(t *testing.T, adminCli *client.Client, uid int32, expectedCapNames []string) {
	t.Helper()
	cs, err := adminCli.GetUserCapabilities(uid)
	require.Nil(t, err)
	assert.ElementsMatch(t, cs.Grants, expectedCapNames)
}
