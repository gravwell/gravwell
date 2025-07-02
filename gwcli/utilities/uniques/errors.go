package uniques

import (
	"errors"
)

// errors shared between packages

// ErrGeneric is intended to be displayed to the user when something goes wrong internally and more details have been logged.
var ErrGeneric = errors.New("an error occurred; see dev.log for more information")

// ErrMustAuth is intended to be displayed to the user whenever they cancel authentication.
var ErrMustAuth = errors.New("you must authenticate to use gwcli")

var ErrBadJWTLength = errors.New("failed to parse JWT; expected splitting on '.' to turn back 3 segments")
