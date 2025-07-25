package connection

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v4/client/objlog"
	"github.com/gravwell/gravwell/v4/ingest/log/rotate"
)

const (
	mb                = 1024 * 1024
	maxLogSize  int64 = 100 * mb
	maxLogCount uint  = 3
)

// restRotator is an adaptor between rotate.FileRotator and objlog.Objlog.
// It allows the rest log to take advantage of log rotation.
type restRotator struct {
	*rotate.FileRotator
}

// fit to the interface
var _ objlog.ObjLog = restRotator{}

// NewRestRotator creates an adaptor between rotate.FileRotator and objlog.Objlog.
// It allows the rest log to take advantage of log rotation.
func NewRestRotator(path string) (restRotator, error) {
	olw := restRotator{}
	var err error
	olw.FileRotator, err = rotate.OpenEx(path, 0660, maxLogSize, maxLogCount, true)
	if err != nil {
		return olw, err
	}

	return olw, nil
}

// Log replicates the output of the JSONObjLogger and passes it to the FileRotator.
func (olw restRotator) Log(id, method string, obj any) error {
	if olw.FileRotator == nil {
		return errors.New("FileRotator not initialized")
	}
	b, err := json.MarshalIndent(obj, "", "\t")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(olw, "%s %s:\n%s\n", id, method, b)
	return err
}
