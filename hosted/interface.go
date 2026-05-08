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
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

// Ingester is the interface that every ingester must implement
type Ingester interface {
	Run(context.Context, Runtime) error
}

// Runner is a wrapper around the Ingester that also implements a Closer so that we can start and stop
type Runner interface {
	Ingester
	Start() error
	Close() error
	Running() bool
	ID() string
	Name() string
	UUID() uuid.UUID
}

// Runtime is the interface provided to a hosted ingester which enables it to
type Runtime interface {
	// Alive indicates whether the upstream ingest connection is alive and healthy
	// a hosted ingester does not have to respect this, but it can help ingester identify when it should back off
	// and maybe sleep more
	Alive() bool
	Sleep(time.Duration) bool // a sleep implementation that an abort early due to context cancellation
	Context() context.Context // grab the global context
	Storage                   // Storage interface
	Logger                    // Logger interface which is a trimmed down surface of github.com/gravwell/gravwell/ingest/log
	Writer
}

type Writer interface {
	// Write will return an error if it failed. Failures are typically due to uplinks being down and/or caches being full.
	// Callers should deal with write errors even though the host will do it's best to receive and cache entries.
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
// it enforces fully structured logging to remove the opportunity to sling poorly formed logs
type Logger interface {
	Debug(msg string, sds ...rfc5424.SDParam)
	Info(msg string, sds ...rfc5424.SDParam)
	Warn(msg string, sds ...rfc5424.SDParam)
	Error(msg string, sds ...rfc5424.SDParam)
	Critical(msg string, sds ...rfc5424.SDParam)
}
