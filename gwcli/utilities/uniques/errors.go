package uniques

import (
	"errors"
)

// errors shared between packages

// ErrMustAuth is intended to be displayed to the user whenever they cancel authentication.
var ErrMustAuth = errors.New("you must authenticate to use gwcli")
