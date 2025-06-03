package connection

import "errors"

var ErrAPITokenRequired error = errors.New("MFA is enabled, API token is required")
var ErrCredentialsOrAPITokenRequired error = errors.New("Credentials or API token required")
var ErrNotInitialized error = errors.New("client must be initialized")
