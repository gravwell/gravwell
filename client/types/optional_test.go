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
	var s string
	bio.Apply(&s)
	require.Equal(t, bio.Value(), s)
	bio.Unset()
	require.False(t, bio.IsSet())
	require.Equal(t, "", bio.Value())
	require.True(t, bio.IsZero())
	bio.Apply(&s)
	require.Equal(t, bio.Value(), s)
}

// NOTE: the marshaler tests for Optional are in client/types/marshallers_test.go
