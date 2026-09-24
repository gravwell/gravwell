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

// NOTE: the marshaler tests for Optional are in client/types/marshallers_test.go
