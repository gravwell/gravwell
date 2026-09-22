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

// NOTE: the marshaler tests for Nullable are in client/types/marshallers_test.go
