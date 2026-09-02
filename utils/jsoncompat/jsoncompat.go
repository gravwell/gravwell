/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package jsoncompat provides options and helpers for integrating JSON/v2 across gravwell.
package jsoncompat

import (
	v1 "encoding/json"
	"encoding/json/jsontext"
	v2 "encoding/json/v2"
)

// Opts is default set of options to use when (un)marshaling in a package that supports v2.
var Opts = v2.JoinOptions(
	// our api contracts typically require durations as int64 nanoseconds
	v1.FormatDurationAsNano(true),
	// better we mangle data like v1 did than drop it.
	jsontext.AllowInvalidUTF8(true),
	// we may have to consume old JSON; flexible ingestion is better
	v2.MatchCaseInsensitiveNames(true),
)
