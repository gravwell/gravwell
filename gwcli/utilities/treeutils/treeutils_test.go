/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package treeutils provides functions for creating the cobra command tree.
// It has been extracted into its own package to avoid import cycles.
package treeutils

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/spf13/cobra"
)

func TestGenerateNav(t *testing.T) {
	// generate some child navs and actions
	const childNavCount uint = 4
	childNavs := make([]*cobra.Command, childNavCount)
	for i := range childNavCount {
		childNavs[i] = GenerateNav(fmt.Sprintf("child_nav_%d", i), fmt.Sprintf("child_nav_%d short", i), fmt.Sprintf("child_nav_%d long", i),
			nil, nil, nil)
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
				nav := GenerateNav("test", "short test", "long test", []string{"alias1", "alias2"},
					childNavs[:tt.navCount],
					nil)

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

}
