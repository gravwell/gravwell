/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package log

import (
	"github.com/crewjam/rfc5424"
)

type KVLogger struct {
	*Logger
	sds []rfc5424.SDParam
}

func NewLoggerWithKV(l *Logger, sds ...rfc5424.SDParam) *KVLogger {
	return &KVLogger{
		Logger: l,
		sds:    sds,
	}
}

// Debug writes a DEBUG level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (kvl *KVLogger) Debug(msg string, sds ...rfc5424.SDParam) error {
	return kvl.outputStructured(DEFAULT_DEPTH+1, DEBUG, msg, append(kvl.sds, sds...)...)
}

// Infof writes an INFO level log to the underlying writer using a format string,
// if the logging level is higher than DEBUG no action is taken
func (kvl *KVLogger) Info(msg string, sds ...rfc5424.SDParam) error {
	return kvl.outputStructured(DEFAULT_DEPTH+1, INFO, msg, append(kvl.sds, sds...)...)
}

// Warn writes an WARN level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (kvl *KVLogger) Warn(msg string, sds ...rfc5424.SDParam) error {
	return kvl.outputStructured(DEFAULT_DEPTH+1, WARN, msg, append(kvl.sds, sds...)...)
}

// Error writes an ERROR level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (kvl *KVLogger) Error(msg string, sds ...rfc5424.SDParam) error {
	return kvl.outputStructured(DEFAULT_DEPTH+1, ERROR, msg, append(kvl.sds, sds...)...)
}

// Critical writes a CRITICALinfo level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (kvl *KVLogger) Critical(msg string, sds ...rfc5424.SDParam) error {
	return kvl.outputStructured(DEFAULT_DEPTH+1, CRITICAL, msg, append(kvl.sds, sds...)...)
}

// AddKVs allows for adding additional KVs to the KV logger
func (kvl *KVLogger) AddKV(sds ...rfc5424.SDParam) {
	kvl.sds = append(kvl.sds, sds...)
}
