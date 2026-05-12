package connection

import "errors"

var (
	ErrAPITokenRequired                     error = errors.New("MFA is enabled, API token is required")
	ErrNotInitialized                       error = errors.New("client must be initialized")
	ErrAPITokenInvalid                      error = errors.New("API key could not be validated")
	ErrMFASetupRequired                     error = errors.New("MFA is required. Please log in via the browser to set it up.")
	ErrInvalidCredentials                   error = errors.New("failed to authenticate with the given credentials")
	ErrNonInteractiveRequiresDifferentLogin error = errors.New("non-interactive mode requires one of the following login methods:\n" +
		"1) explicit username and password (-u)\n" +
		"2) an API token (--api/--eapi)\n" +
		"3) or a valid session from a prior, successful login")
)
