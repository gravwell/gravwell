//go:build noci

package self_test

import (
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

func TestSearchGroup(t *testing.T) {
	var sbOut, sbErr strings.Builder
	meta := testsupport.MetaArgs(t, false, testsupport.WithDefaults())
	// ensure that --set and --clear are MX
	require.NotZero(t, tree.Execute(append(meta, "self", "search-group", "--clear", "--set"), &sbOut, &sbErr))
	require.Contains(t, sbErr.String(), "mutually exclusive")
	sbOut.Reset()
	sbErr.Reset()
	groupName := "grp_" + t.Name() + randomdata.Digits(8)
	// create a group to add ourselves to
	require.Zero(t, tree.Execute(append(meta, "admin", "groups", "create", "-n="+groupName), &sbOut, &sbErr))
	// scan out the group ID
	var GID int
	n, err := fmt.Sscanf(sbOut.String(), "successfully created group (ID: %d)", &GID)
	require.Nil(t, err)
	require.Equal(t, 1, n)
	// associate ourselves to the new group
	sbOut.Reset()
	sbErr.Reset()
	require.Zero(t, tree.Execute(append(meta, "admin", "groups", "associate",
		"--gid="+strconv.FormatInt(int64(GID), 10),
		"--uid=1"), &sbOut, &sbErr), sbErr.String())
	t.Logf("associated admin to group %s (ID: %d)", groupName, GID)
	// kill any existing search groups
	sbOut.Reset()
	sbErr.Reset()
	require.Zero(t, tree.Execute(append(meta, "self", "search-group", "--clear"), &sbOut, &sbErr))
	// check that we have no default search groups
	checkDefaultSearchGroups(t, []int{})
	// set our default search group to the newly created group
	sbOut.Reset()
	sbErr.Reset()
	require.Zero(t, tree.Execute(append(meta, "self", "search-group", "--set", strconv.FormatInt(int64(GID), 10)), &sbOut, &sbErr))
	// check that we now have a search group
	checkDefaultSearchGroups(t, []int{GID})
	// kill the search group to verify --clear
	sbOut.Reset()
	sbErr.Reset()
	require.Zero(t, tree.Execute(append(meta, "self", "search-group", "--clear"), &sbOut, &sbErr))
	checkDefaultSearchGroups(t, []int{})
}

func checkDefaultSearchGroups(t *testing.T, expectedGIDs []int) {
	t.Helper()
	meta := testsupport.MetaArgs(t, false, testsupport.WithDefaults())
	var sbOut, sbErr strings.Builder
	require.Zero(t, tree.Execute(append(meta, "self", "search-group"), &sbOut, &sbErr))
	actualGIDs := []int{}
	out := strings.TrimSpace(sbOut.String())
	out = strings.Trim(out, "[]")
	for s := range strings.FieldsSeq(out) {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		gid, err := strconv.ParseInt(s, 10, 32)
		assert.Nil(t, err)
		actualGIDs = append(actualGIDs, int(gid))
	}
	require.ElementsMatch(t, actualGIDs, expectedGIDs)

}
