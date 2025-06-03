package connection

import "errors"

var (
	ErrAPITokenRequired              error = errors.New("MFA is enabled, API token is required")
	ErrCredentialsOrAPITokenRequired error = errors.New("Credentials or API token required")
	ErrNotInitialized                error = errors.New("client must be initialized")
	ErrAPIKeyInvalid                 error = errors.New("API key could not be validated")
	ErrMFASetupRequired              error = errors.New("MFA is required. Please log in via the browser to set it up.")
)
