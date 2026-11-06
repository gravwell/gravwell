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
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
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
	Version() string // keeping this as a string because some engines may not have good canonical versions
	UUID() uuid.UUID
	Config() any

	// ChildState reports the current state of this single hosted ingester so that it can be
	// registered as a child on the shared ingest muxer.  Callers that know the plugin kind
	// are expected to overwrite Name and Label, a runner only knows its own ID and instance name.
	ChildState() ingest.IngesterState
}

// State are the per ingester ingest counters (and optional error) that a Runtime tracks on behalf of a single
// hosted ingester.  A hosted ingester shares an ingest muxer with every other plugin in the
// process, so the only place these counters can be gathered is the runtime handed to it.
// Error and Warn are transient conditions set by the plugin itself, an ingester that is in an
// error state is still alive and still being scheduled, it just is not doing useful work.
type State struct {
	Entries uint64
	Size    uint64
	Tags    []string
	Error   error
	Warn    string
}

// StatusReporter lets a hosted ingester flag that it is degraded but still alive.  A plugin that
// cannot authenticate, or whose remote API is failing, sets an error and clears it once it
// recovers so that the condition rides along with the ingester state instead of only landing in
// the logs.  StatusTracker is a ready to use implementation.
type StatusReporter interface {
	SetError(error) // flag an error condition, a nil error clears it
	ClearError()    // clear any error condition
	SetWarn(string) // flag a warning condition, an empty string clears it
	ClearWarn()     // clear any warning condition
}

// StateProvider is implemented by Runtimes that track ingest counters for a single hosted
// ingester.  Runtimes are not required to implement it, a runner that is handed one that
// does not simply reports empty counters.
type StateProvider interface {
	State() State
}

// StateUnwrapper is implemented by Runtime wrappers that can hand back the StateProvider of the
// Runtime they wrap.  A wrapper embeds the Runtime interface, so only Runtime's method set is
// promoted and a type assertion for StateProvider against the wrapper can never succeed no matter
// what is underneath it.  Wrappers expose the provider instead.  A wrapper around a Runtime that
// tracks nothing hands back a stub rather than nothing, callers never have to check.
type StateUnwrapper interface {
	StateProvider() StateProvider
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
	StatusReporter // lets an ingester flag a transient error or warning condition
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

// Config is the interface that ingester configuration types must implement so we can detect config changes
type Config interface {
	Equal(any) bool
}

// ConfigSanitizer is an OPTIONAL interface for plugin configurations that are able to hand back
// a version of themselves that is safe to report upstream.  Plugin configs hold API keys, client
// secrets, and tokens, so a config is never reported on the strength of a plugin author having
// remembered to scrub it.  A config that does not implement this interface simply has nothing
// reported, which is the safe default.  Implementations should build an explicit struct holding
// only the fields that are safe rather than returning the config itself, so that a field added
// later cannot quietly leak.
type ConfigSanitizer interface {
	SanitizedConfig() any
}

// EqualTarget normalizes the value handed to a Config.Equal implementation down to a *T.
// Equal takes an any so that configs of differing types can be compared, which means every
// implementation has to deal with being handed a value, a pointer, a nil, or something else
// entirely. Callers get back a usable pointer and true only when the value really is a T,
// everything else is not equal by definition.
func EqualTarget[T any](v any) (*T, bool) {
	if p, ok := v.(*T); ok {
		if p == nil {
			return nil, false
		}
		return p, true
	}
	if val, ok := v.(T); ok {
		return &val, true
	}
	return nil, false
}
