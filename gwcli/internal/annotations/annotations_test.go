package annotations_test

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/internal/annotations"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/gravwell/gravwell/v4/utils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestRequirements(t *testing.T) {
	// apply requirements and then test that CheckRequirements returns as expected
	tests := []struct {
		name string

		requirements annotations.Requirements // Only xpermissions should be respected.

		CBACEnabled bool
		userIsAdmin bool
		usersCaps   []types.Capability

		wantErrContains string // "" means error should be nil. Anything else will check that the error contains the given text
	}{
		{"no requirements; no state",
			annotations.Requirements{},
			false,
			false,
			nil,
			"",
		},
		{"interactive permissions should be ignored",
			annotations.Requirements{IPermissions: []types.Capability{types.ExtractorWrite, types.Download}},
			false,
			false,
			nil,
			"",
		},
		{"no requirements; CBAC enabled, admin, random cap",
			annotations.Requirements{},
			true,
			true,
			[]types.Capability{types.ActionableRead, types.FileWrite}, // just some random caps, shouldn't matter
			"",
		},
		{"requires admin; !admin",
			annotations.Requirements{UserIsAdmin: true},
			false,
			false,
			nil,
			"requires admin",
		},
		{"requires admin; admin",
			annotations.Requirements{UserIsAdmin: true},
			false,
			true,
			nil,
			"",
		},
		{"requires CBAC enabled; it is not",
			annotations.Requirements{DeploymentHasCBAC: true},
			false,
			true, // should be irrelevant
			nil,
			"CBAC",
		},
		{"requires CBAC enabled; and it is",
			annotations.Requirements{DeploymentHasCBAC: true},
			true,
			true, // should be irrelevant
			nil,
			"",
		},
		{"requires several permissions; CBAC is disabled. User has all permissions.",
			annotations.Requirements{XPermissions: []types.Capability{types.Ingest, types.LogbotAI}},
			false,
			false,
			types.CapabilityList(), // should be irreleavnt, hence the next test
			"",
		},
		{"requires several permissions; CBAC is disabled. User should be considered to have all permissions.",
			annotations.Requirements{XPermissions: []types.Capability{types.Ingest, types.LogbotAI}},
			false,
			false,
			nil, // should be irreleavnt, hence the prior test
			"",
		},
		{"requires several permissions; CBAC is disabled. User should be allowed as they are an admin",
			annotations.Requirements{XPermissions: []types.Capability{types.Ingest, types.LogbotAI}},
			false,
			true,
			nil,
			"",
		},
		{"requires several permissions; CBAC is enabled. User has none and is not an admin",
			annotations.Requirements{XPermissions: []types.Capability{types.Ingest, types.LogbotAI}},
			true,
			false,
			nil,
			"requires missing permissions",
		},
		{"requires several permissions; CBAC is enabled. User has some permissions and is not an admin",
			annotations.Requirements{XPermissions: []types.Capability{types.Ingest, types.LogbotAI}},
			true,
			false,
			[]types.Capability{types.LogbotAI},
			"requires missing permissions",
		},
		{"requires several permissions; CBAC is enabled. User has some permissions but is an admin",
			annotations.Requirements{XPermissions: []types.Capability{types.Ingest, types.LogbotAI}},
			true,
			true,
			[]types.Capability{types.LogbotAI},
			"",
		},
		{"requires several permissions; CBAC is enabled. User has all permissions (out of order)",
			annotations.Requirements{
				IPermissions: []types.Capability{types.Ingest, types.LogbotAI, types.Download, types.KitWrite}, // should be ignored
				XPermissions: []types.Capability{types.Ingest, types.LogbotAI},
			},
			true,
			false,
			[]types.Capability{types.LogbotAI, types.Ingest},
			"",
		},
		{"requires no caps; CBAC is enabled",
			annotations.Requirements{},
			true,
			false,
			[]types.Capability{types.LogbotAI, types.Ingest},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// generate a dummy command
			cmd := treeutils.GenerateNav("test", "test", "test", nil, nil, treeutils.NodeOptions{Requirements: tt.requirements})
			// apply annotations
			gotErr := annotations.CheckRequirements(cmd, tt.CBACEnabled, tt.userIsAdmin, uniques.SliceToSet(tt.usersCaps))

			if gotErr == nil && tt.wantErrContains == "" {
				return
			} else if gotErr == nil && tt.wantErrContains != "" {
				t.Fatalf("expected error to contain \"%v\", got nil", tt.wantErrContains)
			} else if gotErr != nil {
				if tt.wantErrContains == "" {
					t.Fatal("expected nil error, got ", gotErr)
				}
				if !strings.Contains(gotErr.Error(), tt.wantErrContains) {
					t.Fatalf("error \"%v\" does not contain expected phrase \"%s\"", gotErr, tt.wantErrContains)
				}
			}
		})
	}
}

func TestConsolidateToDisabled(t *testing.T) {
	// generate a random tree and randomly assign requirements
	root := testsupport.GenerateKDTree(5, 3, func() annotations.Requirements {
		rqs := annotations.Requirements{
			UserIsAdmin:       rand.N(10) < 3,
			DeploymentHasCBAC: rand.N(10) < 2,
		}
		if rand.N(10) < 3 {
			for range rand.N(20) {
				// some caps no longer exist so we need to skip
				var cap types.Capability
				for {
					cap = types.Capability(rand.N(types.LogbotAI))
					if cap.Valid() && cap.String() != "UNKNOWN" {
						break
					}
				}

				rqs.IPermissions = append(rqs.IPermissions, cap)
			}
			rqs.IPermissions = utils.Deduplicate(rqs.IPermissions)
		}
		return rqs
	})

	tests := []struct {
		name string

		cbacEnabled bool
		userIsAdmin bool
		usersCaps   []types.Capability
	}{
		{"cbac enabled, user is admin, user has all caps",
			true, true, types.CapabilityList()},
		{"cbac disabled, user is admin, caps should be irrelevant",
			false, true, nil},
		{"cbac disabled, user is not an admin, caps should be irrelevant",
			false, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			annotations.ConsolidateToDisabled(root, tt.cbacEnabled, tt.userIsAdmin, uniques.SliceToSet(tt.usersCaps))
			checkIsDisabledRecursive(t, root, tt.cbacEnabled, tt.userIsAdmin, tt.usersCaps)
		})
	}

}

// checks that the given command's CheckRequirement result match their IsDisabled state, then recurs to each child.
func checkIsDisabledRecursive(t *testing.T, cmd *cobra.Command, CBACEnabled bool, userIsAdmin bool, usersCapabilities []types.Capability) {
	// check self
	err := annotations.CheckRequirements(cmd, CBACEnabled, userIsAdmin, uniques.SliceToSet(usersCapabilities))
	// error state and IsDisabled state should match
	reason := annotations.IsDisabled(cmd)
	if err != nil {
		assert.Equal(t, err.Error(), reason, "child annotations: %v", cmd.Annotations)
	} else {
		assert.Empty(t, reason, "child annotations: %v", cmd.Annotations)
	}
	// check children
	for _, child := range cmd.Commands() {
		checkIsDisabledRecursive(t, child, CBACEnabled, userIsAdmin, usersCapabilities)
	}
}
