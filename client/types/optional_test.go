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

func TestOptional(t *testing.T) {
	bio := types.NewOptional("biologist")
	require.True(t, bio.IsSet())
	require.Equal(t, "biologist", bio.Value())
	require.False(t, bio.IsZero())
	t.Run("Apply operates when IsSet", func(t *testing.T) {
		var s string
		bio.Apply(&s)
		require.Equal(t, bio.Value(), s)
	})

	bio.Unset()
	require.False(t, bio.IsSet())
	require.Equal(t, "", bio.Value())
	require.True(t, bio.IsZero())
	t.Run("Apply is a no-op when !IsSet", func(t *testing.T) {
		var s = "psychologist"
		bio.Apply(&s)
		require.Equal(t, "psychologist", s)
	})

}

// TestOptionalString pins Optional[T]'s fmt.Stringer contract: an unset
// Optional prints T's zero value (matching MarshalJSONTo's own
// unset-falls-back-to-zero behavior), and a set value prints its underlying
// value's own string form (delegating to T's Stringer, e.g. time.Time, when
// it has one) rather than a raw struct dump of Optional's private fields.
func TestOptionalString(t *testing.T) {
	var o types.Optional[string]
	require.Equal(t, "", o.String())

	o.Set("biologist")
	require.Equal(t, "biologist", o.String())

	o.Unset()
	require.Equal(t, "", o.String())

	ts := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	ot := types.NewOptional(ts)
	require.Equal(t, ts.String(), ot.String())

	var otZero types.Optional[time.Time]
	require.Equal(t, time.Time{}.String(), otZero.String())
}

// TestOptionalGobRoundTrip is a regression test for gravwell/issues#2797:
// Optional[T] has the same private-fields shape as Nullable[T] (see
// TestNullableGobRoundTrip), so it needs the same GobEncode/GobDecode
// treatment or encoding/gob refuses to encode it at all.
func TestOptionalGobRoundTrip(t *testing.T) {
	roundTrip := func(t *testing.T, in types.Optional[time.Time]) types.Optional[time.Time] {
		t.Helper()
		var buf bytes.Buffer
		require.NoError(t, gob.NewEncoder(&buf).Encode(in))
		var out types.Optional[time.Time]
		require.NoError(t, gob.NewDecoder(&buf).Decode(&out))
		return out
	}

	t.Run("unset", func(t *testing.T) {
		var o types.Optional[time.Time]
		out := roundTrip(t, o)
		require.False(t, out.IsSet())
	})

	t.Run("set", func(t *testing.T) {
		ts := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
		o := types.NewOptional(ts)
		out := roundTrip(t, o)
		require.True(t, out.IsSet())
		require.True(t, out.Value().Equal(ts))
	})

	// The actual reported failure: gob-encoding a struct that *contains* an
	// Optional[T] field, not the Optional value directly.
	t.Run("embedded in a struct", func(t *testing.T) {
		type wrapper struct {
			Name  string
			Email types.Optional[string]
		}
		in := wrapper{Name: "joe", Email: types.NewOptional("joe@example.com")}

		var buf bytes.Buffer
		require.NoError(t, gob.NewEncoder(&buf).Encode(in))
		var out wrapper
		require.NoError(t, gob.NewDecoder(&buf).Decode(&out))

		require.Equal(t, "joe", out.Name)
		require.True(t, out.Email.IsSet())
		require.Equal(t, "joe@example.com", out.Email.Value())
	})
}

// NOTE: the marshaler tests for Optional are in client/types/marshallers_test.go
