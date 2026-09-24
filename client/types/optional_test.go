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

// NOTE: the marshaler tests for Optional are in client/types/marshallers_test.go
