//go:build ci

/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package treeutils provides functions for creating the cobra command tree.
// It has been extracted into its own package to avoid import cycles.
package treeutils_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/internal/cmdutils"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestGenerateNav(t *testing.T) {
	// generate some child navs and actions
	const childNavCount uint = 4
	childNavs := make([]*cobra.Command, childNavCount)
	for i := range childNavCount {
		childNavs[i] = treeutils.GenerateNav(fmt.Sprintf("child_nav_%d", i), fmt.Sprintf("child_nav_%d short", i), fmt.Sprintf("child_nav_%d long", i),
			nil, nil)
	}
	t.Run("usage", func(t *testing.T) {
		tests := []struct {
			name     string
			navCount uint
			expected string
		}{
			{"no children", 0, "test [subcommand]"},
			{"one child", 1, "test {child_nav_0}"},
			{"two children", 2, "test {child_nav_0|child_nav_1}"},
			{"three children", 3, "test {child_nav_0|child_nav_1|child_nav_2}"},
			{"many children", 4, "test {child_nav_0|child_nav_1|child_nav_2|...}"},
		}
		for _, tt := range tests {
			// sanity check the test
			if tt.navCount > childNavCount {
				t.Skipf("too many navs request (request: %d | available: %d)", tt.navCount, childNavCount)
			}
			t.Run(tt.name, func(t *testing.T) {
				nav := treeutils.GenerateNav("test", "short test", "long test",
					childNavs[:tt.navCount],
					nil, treeutils.NodeOptions{CommandAliases: []string{"alias1", "alias2"}})

				var sbOut strings.Builder
				nav.SetOut(&sbOut)
				if err := nav.Usage(); err != nil {
					t.Fatal(err)
				}

				if actual := strings.TrimSpace(sbOut.String()); tt.expected != actual {
					t.Error("bad usage.", testsupport.ExpectedActual(tt.expected, actual))
				}
			})
		}
	})

	t.Run("annotations", func(t *testing.T) {
		t.Run("no annotations set", func(t *testing.T) {
			nav := treeutils.GenerateNav("test", "test", "test", nil, nil, treeutils.NodeOptions{})
			assert.False(t, cmdutils.IsAdminOnly(nav))
		})
		t.Run("admin-only annotation set", func(t *testing.T) {
			nav := treeutils.GenerateNav("test", "test", "test", nil, nil, treeutils.NodeOptions{AdminOnly: true})
			assert.True(t, cmdutils.IsAdminOnly(nav))
		})
		t.Run("adminOnly is hereditary", func(t *testing.T) {
			gn := func(idx int, childNavs []*cobra.Command, childActions []action.Pair) *cobra.Command {
				sidx := strconv.FormatInt(int64(idx), 32)
				return treeutils.GenerateNav(sidx, sidx, sidx, childNavs, childActions)
			}
			// generate some children, none marked as admin only
			root := treeutils.GenerateNav("root", "root", "root", []*cobra.Command{
				gn(11, nil, nil),
				gn(12, nil, []action.Pair{
					dummyActionPair(treeutils.GenerateActionOptions{}),
				})},
				[]action.Pair{
					dummyActionPair(treeutils.GenerateActionOptions{}),
				},
				treeutils.NodeOptions{AdminOnly: true},
			)
			// root and everything under it should be marked as admin
			assert.True(t, cmdutils.IsAdminOnly(root))
			for _, cmd := range root.Commands() {
				assert.True(t, cmdutils.IsAdminOnly(cmd), cmd.Name())
			}
		})
	})
}

// dummyActionPair returns an action.Pair that does nothing and has no model.
func dummyActionPair(opts treeutils.GenerateActionOptions) action.Pair {
	x := randomdata.City()
	return action.NewPair(treeutils.GenerateAction(x, x, x, func(c *cobra.Command, s []string) error { return nil }, opts), nil)
}
