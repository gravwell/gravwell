/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

const (
	Info  = "info"
	Warn  = "warn"
	Error = "error"
	Fatal = "fatal"
)

// Unique Message IDs.
const (
	MessageMisaligned   = iota // Misaligned time window request
	MessageInvalidRange        // Invalid time range
)

// A Message is a general use type for communicating various forms of errors,
// warnings, etc. It's primary use is on the render APIs to communicate search
// errors. Messages must set the appropriate ID from the list above, as the GUI
// uses these for localization and keying.
type Message struct {
	ID       uint64 // Unique ID from the list above
	Severity string // One of "info", "warn", "error", or "fatal"
	Value    string // Message contents
}
