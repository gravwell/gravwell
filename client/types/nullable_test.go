/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types_test

import (
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/stretchr/testify/require"
)

func TestNullable(t *testing.T) {
	n := types.NewNullable("biologist")
	require.False(t, n.IsNull())
	require.Equal(t, "biologist", n.Value())
	require.False(t, n.IsZero())

	n.SetNull()
	require.True(t, n.IsNull())
	require.Equal(t, "", n.Value())
	// IsZero must stay false even when null: Nullable is never omitted by
	// `,omitzero`, only Optional is.
	require.False(t, n.IsZero())

	n.Set("psychologist")
	require.False(t, n.IsNull())
	require.Equal(t, "psychologist", n.Value())
}

// TestNullableZeroValueIsNull pins that the Go zero value of a Nullable
// (i.e. a struct field that was never explicitly initialized) behaves as
// null, matching the default "not deleted"/"no value" state callers expect
// without having to construct one explicitly.
func TestNullableZeroValueIsNull(t *testing.T) {
	var n types.Nullable[string]
	require.True(t, n.IsNull())
	require.Equal(t, "", n.Value())
}

// TestNullableString pins Nullable[T]'s fmt.Stringer contract: a null value
// prints the literal "null", and a non-null value prints its underlying
// value's own string form (delegating to T's Stringer, e.g. time.Time, when
// it has one) rather than a raw struct dump of Nullable's private fields.
// This is what utils/weave's CSV/table output actually calls when
// stringifying a Nullable field via %v/fmt.Sprint.
func TestNullableString(t *testing.T) {
	var n types.Nullable[string]
	require.Equal(t, "null", n.String())

	n.Set("biologist")
	require.Equal(t, "biologist", n.String())

	n.SetNull()
	require.Equal(t, "null", n.String())

	ts := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	nt := types.NewNullable(ts)
	require.Equal(t, ts.String(), nt.String())

	var ntZero types.Nullable[time.Time]
	require.Equal(t, "null", ntZero.String())
}

// NOTE: the marshaler tests for Nullable are in client/types/marshallers_test.go
