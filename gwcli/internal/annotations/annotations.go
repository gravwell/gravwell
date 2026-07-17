// Package annotations controls the annotations used to mark cobra.Commands to alter their handling.
package annotations

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/state"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
)

const (
	keyUserIsAdmin       string = "user_is_admin"
	keyDeploymentHasCBAC string = "deployment_has_CBAC"
	keyPermissions       string = "permissions"
	keyDisabled          string = "disabled"
)
const requirementValue string = "1"

// Requirements define the requirements that must be satisfied for a command to be invoked.
//
// Requirements have overlapping conditions; you probably only want to set one requirement property (ex: Permissions or UserIsAdmin, not both).
type Requirements struct {
	// Requires that the user is an admin.
	UserIsAdmin bool
	// Requires that the deployment has CBAC enabled, but not that the user has any specific permissions.
	// This is likely to be only useful for the CBAC nav itself.
	DeploymentHasCBAC bool
	// CBAC permissions the user must have to execute this action.
	// Being an admin overrules any permissions set here.
	//
	// If CBAC is disabled, the user is considered to have all permissions.
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

	if state.DebugMode() { // sanity checks
		if r.DeploymentHasCBAC && (r.UserIsAdmin || len(r.Permissions) > 0) {
			clilog.Writer.Warn("conflicting requirements: deploymentHasCBAC should not be paired with userIsAdmin or permissions",
				log.KV("cmd", cmd.Name()),
				log.KV("userIsAdmin", r.UserIsAdmin),
				log.KV("permissions", r.Permissions),
			)
		}
		if r.UserIsAdmin && len(r.Permissions) > 0 {
			clilog.Writer.Warn("conflicting requirements: userIsAdmin obviates permissions",
				log.KV("cmd", cmd.Name()),
				log.KV("permissions", r.Permissions),
			)
		}
	}

	if r.UserIsAdmin {
		cmd.Annotations[keyUserIsAdmin] = requirementValue
	}
	if r.DeploymentHasCBAC {
		cmd.Annotations[keyDeploymentHasCBAC] = requirementValue
	}
	if len(r.Permissions) > 0 {
		// convert each cap to its string form
		requiredCaps := make([]string, len(r.Permissions))
		for i, p := range r.Permissions {
			requiredCaps[i] = p.Name()
		}
		cmd.Annotations[keyPermissions] = strings.Join(requiredCaps, ",")
	}

}

// RequirementsStrings extracts the requirements inherent to the cmd and returns them as an array of ordered string.
// Intended to help declare a command's requirements for a user.
func RequirementsStrings(cmd *cobra.Command) []string {
	if cmd.Annotations == nil {
		return nil
	}
	rqs := make([]string, 0, len(cmd.Annotations))
	if _, deploymentRequired := cmd.Annotations[keyDeploymentHasCBAC]; deploymentRequired {
		rqs = append(rqs, "Requires CBAC to be enabled.")
	}
	if _, adminRequired := cmd.Annotations[keyUserIsAdmin]; adminRequired {
		rqs = append(rqs, "Requires admin privileges.")
	}
	if capsStr, permissionsRequired := cmd.Annotations[keyPermissions]; permissionsRequired {
		rqs = append(rqs, "Requires CBAC permissions: "+capsStr)
	}
	return slices.Clip(rqs)
}

// CheckRequirements tests if the user and deployment satisfies all requirements to execute this given command.
//
// All params other than cmd can and should be pulled from the connection singleton.
// userCapabilities is a set (capability.String -> true).
func CheckRequirements(cmd *cobra.Command, CBACEnabled bool, userIsAdmin bool, usersCapabilities map[types.Capability]bool) error {
	if cmd.Annotations == nil { // can't have requirements if you don't have annotations
		return nil
	}
	if _, deploymentRequired := cmd.Annotations[keyDeploymentHasCBAC]; deploymentRequired && !CBACEnabled {
		return errors.New("'" + cmd.Name() + "' requires CBAC to be enabled. See https://docs.gravwell.io/cbac/cbac.html for more information")
	}

	if userIsAdmin { // admin users can perform any action
		return nil
	}

	// UserIsAdmin requirement always applies
	if _, adminRequired := cmd.Annotations[keyUserIsAdmin]; adminRequired {
		return errors.New("'" + cmd.Name() + "' requires admin privileges")
	}

	// Users are considered to have all permissions when CBAC is disabled.
	if !CBACEnabled {
		return nil
	}

	// CBAC is enabled. Check that the user has all required permissions.
	capsStr, found := cmd.Annotations[keyPermissions]
	if !found { // command requires no caps
		return nil
	}
	var missingCaps []string
	for requiredCap := range strings.SplitSeq(capsStr, ",") {
		cap := types.Capability(0)
		if err := cap.Parse(requiredCap); err != nil {
			clilog.Writer.Error("required capability failed to parse",
				log.KV("raw string", requiredCap),
				log.KVErr(err),
			)
		}

		if _, found := usersCapabilities[cap]; !found {
			missingCaps = append(missingCaps, requiredCap)
		}
	}
	if len(missingCaps) > 0 {
		return fmt.Errorf("'%s' requires missing permissions: %v", cmd.Name(), missingCaps)
	}
	// user has all caps required by the command
	return nil
}

// ConsolidateToDisabled checks the given command (and recurs down each of its branches) to see if its requirements are currently satisfied.
// If they are not, the command is marked with the 'disabled' annotation.
// The annotation's value is the reason it is disabled.
func ConsolidateToDisabled(cmd *cobra.Command, CBACEnabled bool, userIsAdmin bool, usersCapabilities map[types.Capability]bool) {
	if cmd == nil {
		return
	}
	// CheckRequirements checks that the anno map is not nil for us
	if err := CheckRequirements(cmd, CBACEnabled, userIsAdmin, usersCapabilities); err != nil {
		cmd.Annotations[keyDisabled] = err.Error()
	}
	for _, child := range cmd.Commands() {
		ConsolidateToDisabled(child, CBACEnabled, userIsAdmin, usersCapabilities)
	}
}

// IsDisabled returns the reason this command is disabled (or the empty string).
// Commands only read as disabled if ConsolidateToDisabled as been executed against the command tree.
func IsDisabled(cmd *cobra.Command) (reason string) {
	if cmd == nil {
		clilog.Writer.Warn("IsDisabled called on a nil command", log.KV("caller", log.CallLoc(1)))
		return ""
	} else if len(cmd.Annotations) < 1 {
		return ""
	}
	return cmd.Annotations[keyDisabled]
}
