package uniques

import "errors"

// errors shared between packages

// ErrGeneric is intended to be displayed to the user when something goes wrong internally and more details have been logged.
var ErrGeneric = errors.New("an error occurred; see dev.log for more information")
