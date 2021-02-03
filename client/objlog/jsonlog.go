/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package objlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

var (
	errNilFout = errors.New("Nil fout file handle")
)

type JSONObjLogger struct {
	fout *os.File
}

// NewJSONLogger initializes an ObjLog compatible interface for logging request and response objects.
// The JSON logger will open a file in append mode and log requests, including their request and response objects.
// The output will be compact JSON encoded objects.
func NewJSONLogger(path string) (ObjLog, error) {
	fout, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0660)
	if err != nil {
		return nil, err
	}
	return &JSONObjLogger{
		fout: fout,
	}, nil
}

// Log records a request path, method, and the objects sent and received.
// The output JSON will be encoded with tab indentation and a newline betwen objects.
func (jol *JSONObjLogger) Log(id, method string, obj interface{}) error {
	if jol.fout == nil {
		return errNilFout
	}
	b, err := json.MarshalIndent(obj, "", "\t")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(jol.fout, "%s %s:\n%s\n", id, method, b)
	return err
}

// Close will flush the file output and close the file handle.
// The caller should not operate on the JSON logger after calling Close.
// Close will return an error if the ojbect is already closed or has not be initalized.
func (jol *JSONObjLogger) Close() error {
	if jol.fout == nil {
		return errNilFout
	}
	if err := jol.fout.Close(); err != nil {
		return err
	}
	jol.fout = nil
	return nil
}
