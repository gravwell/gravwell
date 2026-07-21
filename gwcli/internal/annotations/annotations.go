// Package annotations controls the annotations used to mark cobra.Commands to alter their handling.
package annotations

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dustin/go-humanize/english"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/state"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
)

const (
	keyUserIsAdmin       string = "user_is_admin"
	keyDeploymentHasCBAC string = "deployment_has_CBAC"
	keyIPermissions      string = "interactive_permissions"
	keyXPermissions      string = "x_permissions"
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
	// CBAC permissions the user must have to execute this action *interactively*.
	// Interactive permissions are assumed to be a superset of non-interactive permissions.
	//
	// Being an admin overrules any permissions set here.
	//
	// If CBAC is disabled, the user is considered to have all permissions.
	IPermissions []types.Capability
	// CBAC permissions the user must have to execute this action *non-interactively*.
	// Being an admin overrules any permissions set here.
	//
	// If CBAC is disabled, the user is considered to have all permissions.
	XPermissions []types.Capability
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
		if r.DeploymentHasCBAC && (r.UserIsAdmin || len(r.IPermissions) > 0) {
			clilog.Writer.Warn("conflicting requirements: deploymentHasCBAC should not be paired with userIsAdmin or permissions",
				log.KV("cmd", cmd.Name()),
				log.KV("userIsAdmin", r.UserIsAdmin),
				log.KV("permissions", r.IPermissions),
			)
		}
		if r.UserIsAdmin && len(r.IPermissions) > 0 {
			clilog.Writer.Warn("conflicting requirements: userIsAdmin obviates permissions",
				log.KV("cmd", cmd.Name()),
				log.KV("permissions", r.IPermissions),
			)
		}
	}

	if r.UserIsAdmin {
		cmd.Annotations[keyUserIsAdmin] = requirementValue
	}
	if r.DeploymentHasCBAC {
		cmd.Annotations[keyDeploymentHasCBAC] = requirementValue
	}
	if len(r.IPermissions) > 0 {
		// convert each cap to its string form
		requiredCaps := make([]string, len(r.IPermissions))
		for i, p := range r.IPermissions {
			requiredCaps[i] = p.Name()
		}
		cmd.Annotations[keyIPermissions] = strings.Join(requiredCaps, ",")
	}
	if len(r.XPermissions) > 0 {
		// convert each cap to its string form
		requiredCaps := make([]string, len(r.XPermissions))
		for i, p := range r.XPermissions {
			requiredCaps[i] = p.Name()
		}
		cmd.Annotations[keyXPermissions] = strings.Join(requiredCaps, ",")
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
	var xp []string
	if x := cmd.Annotations[keyIPermissions]; len(x) > 0 {
		xp = strings.Split(x, ",")
	}
	var ip []string
	if i := cmd.Annotations[keyXPermissions]; len(i) > 0 {
		ip = strings.Split(i, ",")
	}

	IExtra := slices.DeleteFunc(ip, func(s string) bool {
		return slices.Contains(xp, s)
	})

	var sb strings.Builder
	if len(xp) > 0 {
		sb.WriteString("Requires CBAC permissions ")
		sb.WriteString(english.WordSeries(xp, "and"))
		if len(IExtra) > 0 {
			sb.WriteString(" (plus ")
			sb.WriteString(english.WordSeries(IExtra, "and"))
			sb.WriteString(" if called without -x)")
		}
		sb.WriteRune('.')

	} else {
		sb.WriteString("Requires no CBAC permissions")
		if len(IExtra) > 0 {
			sb.WriteString(" (requires ")
			sb.WriteString(english.WordSeries(IExtra, "and"))
			sb.WriteString(" if called without -x)")
		}
		sb.WriteRune('.')
	}
	rqs = append(rqs, sb.String())

	return slices.Clip(rqs)
}

// CheckRequirements tests if the user and deployment satisfies all requirements to execute this given command.
// It is intended for use prior to interactive mode; interactive mode should be supported by ConsolidateToDisabled and IsDisabled.
//
// cmd carries the annotations to be checked.
//
// CBACEnabled, userIsAdmin, and usersCapabilities can and should all be pulled from the connection singleton.
// userCapabilities is a set (capability.String -> true).
func CheckRequirements(cmd *cobra.Command, CBACEnabled bool, userIsAdmin bool, usersCapabilities map[types.Capability]bool) error {
	return checkRequirements(cmd, false, CBACEnabled, userIsAdmin, usersCapabilities)
}

// internal implementation of checkRequirements that can be positioned to check IPerms or XPerms.
func checkRequirements(cmd *cobra.Command, interactive bool, CBACEnabled bool, userIsAdmin bool, usersCapabilities map[types.Capability]bool) error {
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
	var kp string
	if interactive {
		kp = keyIPermissions
	} else {
		kp = keyXPermissions
	}

	capsStr, found := cmd.Annotations[kp]
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
//
// Intended for use in interactive mode as it facilitates fast lookups.
func ConsolidateToDisabled(cmd *cobra.Command, CBACEnabled bool, userIsAdmin bool, usersCapabilities map[types.Capability]bool) {
	if cmd == nil {
		return
	}
	// CheckRequirements checks that the anno map is not nil for us
	if err := checkRequirements(cmd, true, CBACEnabled, userIsAdmin, usersCapabilities); err != nil {
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
