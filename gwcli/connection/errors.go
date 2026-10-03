package connection

import (
	"errors"
	"fmt"
	"os"
)

var (
	ErrAPITokenRequired                     error = errors.New("MFA is enabled, API token is required")
	ErrNotInitialized                       error = errors.New("client must be initialized")
	ErrAPITokenInvalid                      error = errors.New("API key could not be validated")
	ErrMFASetupRequired                     error = errors.New("MFA is required. Please log in via the browser to set it up.") //lint:ignore ST1005 user-facing error
	ErrInvalidCredentials                   error = errors.New("failed to authenticate with the given credentials")
	ErrNonInteractiveRequiresDifferentLogin error = errors.New("non-interactive mode requires one of the following login methods:\n" +
		"1) explicit username and password (-u)\n" +
		"2) an API token (--api/--eapi)\n" +
		"3) or a valid session from a prior, successful login")
)

type ErrBadPermissions struct {
	Expected os.FileMode
	Actual   os.FileMode
}

func (e ErrBadPermissions) Error() string {
	return fmt.Sprintf("incorrect permissions. Should be %[1]s(%[1]o), got %[2]s(%[2]o)", e.Expected, e.Actual)
}

func (e ErrBadPermissions) Is(err error) bool {
	_, ok := err.(ErrBadPermissions)
	return ok
}
