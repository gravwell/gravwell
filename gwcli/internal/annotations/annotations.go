// Package annotations controls the annotations used to mark cobra.Commands to alter their handling.
package annotations

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
)

const (
	keyUserIsAdmin       string = "user_is_admin"
	keyDeploymentHasCBAC string = "deployment_has_CBAC"
	keyPermissions       string = "permissions"
)
const requirementValue string = "1"

// Requirements define the requirements that must be satisfied for a command to be invoked.
//
// Requirements have overlapping conditions; you probably only want to set one requirement property (ex: Permissions or UserIsAdmin, not both).
type Requirements struct {
	// Requires that the user is an admin, no matter the state of CBAC.
	UserIsAdmin bool
	// Requires that the deployment has CBAC enabled, but not that the user has any specific permissions.
	// This is likely to be only useful for the CBAC nav itself.
	OnlyDeploymentHasCBAC bool
	// CBAC permissions the user must have to execute this action.
	// Being an admin overrules any permissions set here.
	//
	// If the deployment does not have CBAC enabled, this array containing any items will instead mark it as requiring the user be an admin.
	Permissions []types.Capability
}

// Apply sets annotations on the given command.
func (r Requirements) Apply(cmd *cobra.Command) {
	if cmd == nil {
		clilog.Writer.Warn("nil cmd", log.KV("caller", log.CallLoc(1)))
		return
	}
	if cmd.Annotations == nil {
		cmd.Annotations = make(map[string]string)
	}
	if r.UserIsAdmin {
		cmd.Annotations[keyUserIsAdmin] = requirementValue
	}
	if r.OnlyDeploymentHasCBAC {
		cmd.Annotations[keyDeploymentHasCBAC] = requirementValue
	}
	requiredCaps := make([]string, len(r.Permissions))
	for i, p := range r.Permissions {
		requiredCaps[i] = p.Name()
	}
	cmd.Annotations[keyPermissions] = strings.Join(requiredCaps, ",")
}

// CheckRequirements tests if the user and deployment satisfies all requirements to execute this given command.
func CheckRequirements(cmd *cobra.Command, CBACEnabled bool, userIsAdmin bool, usersCapabilities []types.Capability) error {
	if cmd.Annotations == nil { // can't have requirements if you don't have annotations
		return nil
	}
	if _, deploymentRequired := cmd.Annotations[keyDeploymentHasCBAC]; deploymentRequired && !CBACEnabled {
		return errors.New(cmd.Name() + " requires CBAC be enabled")
	}

	if userIsAdmin { // admin users can perform any action
		return nil
	}

	// UserIsAdmin requirement always applies
	if _, adminRequired := cmd.Annotations[keyUserIsAdmin]; adminRequired {
		return errors.New(cmd.Name() + "requires admin privileges")
	}

	if CBACEnabled { // CBAC is enabled, check that the user has all listed permissions
		capsStr, found := cmd.Annotations[keyPermissions]
		if !found { // command requires no caps
			return nil
		}
		var missingCaps []string
		for requiredCap := range strings.SplitSeq(capsStr, ",") {
			hasPerm := slices.ContainsFunc(usersCapabilities, func(permittedCap types.Capability) bool { return requiredCap == permittedCap.Name() })
			if !hasPerm {
				missingCaps = append(missingCaps, requiredCap)
			}
		}
		if len(missingCaps) > 0 {
			return fmt.Errorf("%s requires missing permissions: %v", cmd.Name(), missingCaps)
		}
		// user has all caps required by the command
		return nil
	}

	// CBAC is disabled. Actions that normally require any CBAC permissions instead require admin.
	if _, anyPermissionsRequired := cmd.Annotations[keyUserIsAdmin]; anyPermissionsRequired && !userIsAdmin {
		return errors.New(cmd.Name() + "requires admin privileges")
	}
	return nil
}
