package annotations_test

import (
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/internal/annotations"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
)

func TestRequirements(t *testing.T) {
	// apply requirements and then test that CheckRequirements returns as expected
	tests := []struct {
		name string

		requirements annotations.Requirements

		CBACEnabled       bool
		userIsAdmin       bool
		usersCapabilities []types.Capability

		wantErrContains string // "" means error should be nil. Anything else will check that the error contains the given text
	}{
		{"no requirements; no state",
			annotations.Requirements{},
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// generate a dummy command
			cmd := treeutils.GenerateNav("test", "test", "test", nil, nil, treeutils.NodeOptions{Requirements: tt.requirements})
			// apply annotations
			gotErr := annotations.CheckRequirements(cmd, tt.CBACEnabled, tt.userIsAdmin, tt.usersCapabilities)

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
