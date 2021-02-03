/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package objlog

// ObjLog is the interfaced used by the command line client so that all API requests and responses
// can be logged during normal operation.  This interface is useful for debugging, development, and
// tracing the Gravwell request API.
type ObjLog interface {
	Close() error
	Log(id, method string, obj interface{}) error
}

// NillObjLogger is an empty implementation of the ObjLog interface for use when no logging is desired.
type NilObjLogger struct {
}

// NewNilLogger generates an empty/do nothing logger that implements the ObjLog interface.
func NewNilLogger() (ObjLog, error) {
	return &NilObjLogger{}, nil
}

// Log implements the Log method on the interface, NilObjLogger does nothing.
func (nol *NilObjLogger) Log(id, method string, obj interface{}) error {
	return nil
}

// Close implements the Close method on the interface, NilObjLogger does nothing.
func (nol *NilObjLogger) Close() error {
	return nil
}
