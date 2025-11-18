/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package hosted implements basic control systems and interfaces for hosted ingesters
package hosted

import (
	"context"
	"errors"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

var (
	ErrStorageNotFound = errors.New("not found") // error returned when a storage item isn't found
)

// Ingester is the interface that every ingester must implement
type Ingester interface {
	Name() string
	Version() version.Canonical
	UUID() uuid.UUID
	Run(Runtime) error
}

// Runner is a wrapper around the Ingester that also implements a Closer so that we can start and stop
type Runner interface {
	Ingester
	Start() error
	Close() error
}

// Runtime is the interface provided to a hosted ingester which enables it to
type Runtime interface {
	// Alive indicates whether the upstream ingest connection is alive and healthy
	// a hosted ingester does not have to respect this, but it can help ingester identify when it should back off
	// and maybe slep more
	Alive() bool
	Sleep(time.Duration) bool // a sleep implementation that an abort early due to context cancellation
	Context() context.Context // grab the context
	Storage                   // Storage interface
	Logger                    // Logger interface which is a trimmed down surface of github.com/gravwell/gravwell/ingest/log
	Writer
}

type Writer interface {
	// error returned if we failed, failures are typically due to uplinks being down and/or caches being full
	// callers should deal with write errors even though the host will do it's best to recieve and cache entries
	Write(entry.Entry) error
	NegotiateTag(name string) (entry.EntryTag, error) // try to negotiate a tag
}

// TagNegotiator is just a limited version of the writer so that a hosted ingester can check tag negotiation at startup
type TagNegotiator interface {
	NegotiateTag(name string) (entry.EntryTag, error)
}

// Storage is the provided interface that enables hosted ingesters to store and retrieve state
type Storage interface {
	Get(string) ([]byte, error)
	Put(string, []byte) error
	GetString(string) (string, error)
	PutString(string, string) error
	GetInt64(string) (int64, error)
	PutInt64(string, int64) error
	GetTime(string) (time.Time, error)
	PutTime(string, time.Time) error
}

// Logger is a cut down interface from github.com/gravwell/gravwell/ingest/log.Logger
// it enforces fully structred logging to remove the opportunity to sling poorly formed logs
type Logger interface {
	Debug(msg string, sds ...rfc5424.SDParam) error
	Info(msg string, sds ...rfc5424.SDParam) error
	Warn(msg string, sds ...rfc5424.SDParam) error
	Error(msg string, sds ...rfc5424.SDParam) error
	Critical(msg string, sds ...rfc5424.SDParam) error
}
