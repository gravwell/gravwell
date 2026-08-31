// Package jsoncompat is a transition package to assist us in making the JSONv1 -> JSONv2 jump.
package jsoncompat

import (
	v1 "encoding/json"
	"encoding/json/jsontext"
	v2 "encoding/json/v2"
)

// Options is the set of options that define how we interact with JSON encoding/decoding.
var Options = v2.JoinOptions(
	// we format time.Durations as int64 nanonseconds
	v1.FormatDurationAsNano(true),
	// v1 subbed in U+FFFD for invalid UTF-8. v2 rejects it outright.
	// We don't ever want to error, so stick with mangling the invalid UTF-8.
	jsontext.AllowInvalidUTF8(true),
	// being strict about case sensitivity could break client scripts and migrations.
	v2.MatchCaseInsensitiveNames(true),
)
