package scaffoldedit

import (
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
)

// ErrUnknownID returns "unknown id <>".
// Intended for use in Select subroutines.
func ErrUnknownID[I scaffold.Id_t](id I) error {
	return fmt.Errorf("unknown id %v", id)
}

// ErrUnknownField returns "unknown field <>".
// Intended for use in Get/SetField subroutines.
func ErrUnknownField(fieldKey string) error {
	return errors.New("unknown field " + fieldKey)
}
