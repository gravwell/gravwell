/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types_test

import (
	"bytes"
	"encoding/gob"
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

// TestNullableGobRoundTrip is a regression test for gravwell/issues#2797:
// Nullable[T]'s fields are private, so without GobEncode/GobDecode,
// encoding/gob refuses to encode it at all -- "gob: type ... has no exported
// fields" -- since gob has no notion of the JSON hooks Nullable relies on
// elsewhere. This bit backend code that gob-encodes CommonFields (and
// therefore its embedded Nullable[time.Time] DeletedAt) for internal
// transport.
func TestNullableGobRoundTrip(t *testing.T) {
	roundTrip := func(t *testing.T, in types.Nullable[time.Time]) types.Nullable[time.Time] {
		t.Helper()
		var buf bytes.Buffer
		require.NoError(t, gob.NewEncoder(&buf).Encode(in))
		var out types.Nullable[time.Time]
		require.NoError(t, gob.NewDecoder(&buf).Decode(&out))
		return out
	}

	t.Run("null", func(t *testing.T) {
		var n types.Nullable[time.Time]
		out := roundTrip(t, n)
		require.True(t, out.IsNull())
	})

	t.Run("set", func(t *testing.T) {
		ts := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
		n := types.NewNullable(ts)
		out := roundTrip(t, n)
		require.False(t, out.IsNull())
		require.True(t, out.Value().Equal(ts))
	})

	// The actual reported failure: gob-encoding a struct that *contains* a
	// Nullable[T] field, not the Nullable value directly.
	t.Run("embedded in a struct", func(t *testing.T) {
		type wrapper struct {
			Name      string
			DeletedAt types.Nullable[time.Time]
		}
		ts := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
		in := wrapper{Name: "asset", DeletedAt: types.NewNullable(ts)}

		var buf bytes.Buffer
		require.NoError(t, gob.NewEncoder(&buf).Encode(in))
		var out wrapper
		require.NoError(t, gob.NewDecoder(&buf).Decode(&out))

		require.Equal(t, "asset", out.Name)
		require.False(t, out.DeletedAt.IsNull())
		require.True(t, out.DeletedAt.Value().Equal(ts))
	})
}

func TestNullableEqual(t *testing.T) {
	var a, b types.Nullable[string]
	require.True(t, a.Equal(b), "two null values should be equal")

	a.Set("x")
	require.False(t, a.Equal(b), "a set value should not equal a null one")

	b.Set("x")
	require.True(t, a.Equal(b), "two sets holding the same value should be equal")

	b.Set("y")
	require.False(t, a.Equal(b), "two sets holding different values should not be equal")
}

// NOTE: the marshaler tests for Nullable are in client/types/marshallers_test.go
