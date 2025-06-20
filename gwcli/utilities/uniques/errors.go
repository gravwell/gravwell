package uniques

import (
	"errors"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
)

// errors shared between packages

// ErrGeneric is intended to be displayed to the user when something goes wrong internally and more details have been logged.
var ErrGeneric = errors.New("an error occurred; see dev.log for more information")

// ErrMustAuth is intended to be displayed to the user whenever they cancel authentication.
var ErrMustAuth = errors.New("you must authenticate to use gwcli")

var ErrBadJWTLength = errors.New("failed to parse JWT; expected splitting on '.' to turn back 3 segments")

// Returns a user-friendly error (errGeneric), but logs a critical error to clilog.
func ErrFlagDNE(flagName string, actionName string) (ufErr error) {
	clilog.Writer.Criticalf("flag '%v' does not exist on given flagset. Action: %v", flagName, actionName)
	return ErrGeneric
}
