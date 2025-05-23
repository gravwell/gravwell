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

// GetKV returns the structured data elements
func (kvl *KVLogger) GetKV() []rfc5424.SDParam {
	return kvl.sds
}

// GetKVMap returns the structured data elements as a map[string]string
func (kvl *KVLogger) GetKVMap() map[string]string {
	m := make(map[string]string)
	for _, sd := range kvl.sds {
		m[sd.Name] = sd.Value
	}
	return m
}

// Debug writes a DEBUG level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (kvl *KVLogger) Debug(msg string, sds ...rfc5424.SDParam) error {
	return kvl.outputStructured(DEFAULT_DEPTH, DEBUG, msg, append(kvl.sds, sds...)...)
}

// Info writes an INFO level log to the underlying writer using a static string,
// if the logging level is higher than DEBUG no action is taken
func (kvl *KVLogger) Info(msg string, sds ...rfc5424.SDParam) error {
	return kvl.outputStructured(DEFAULT_DEPTH, INFO, msg, append(kvl.sds, sds...)...)
}

// Warn writes an WARN level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (kvl *KVLogger) Warn(msg string, sds ...rfc5424.SDParam) error {
	return kvl.outputStructured(DEFAULT_DEPTH, WARN, msg, append(kvl.sds, sds...)...)
}

// Error writes an ERROR level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (kvl *KVLogger) Error(msg string, sds ...rfc5424.SDParam) error {
	return kvl.outputStructured(DEFAULT_DEPTH, ERROR, msg, append(kvl.sds, sds...)...)
}

// Critical writes a CRITICALinfo level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (kvl *KVLogger) Critical(msg string, sds ...rfc5424.SDParam) error {
	return kvl.outputStructured(DEFAULT_DEPTH, CRITICAL, msg, append(kvl.sds, sds...)...)
}

// AddKV allows for adding additional KVs to the KV logger
func (kvl *KVLogger) AddKV(sds ...rfc5424.SDParam) {
	kvl.sds = append(kvl.sds, sds...)
}
