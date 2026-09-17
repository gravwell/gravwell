/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils_test

import (
	"testing"

	"github.com/gravwell/gravwell/v4/utils"
	"github.com/stretchr/testify/require"
)

func TestDeduplicate(t *testing.T) {
	t.Run("duplicates in multiples and intermixed", func(t *testing.T) {
		arr := []string{"a", "b", "c", "b", "b", "b", "d", "a"}
		want := []string{"a", "b", "c", "d"}
		got := utils.Deduplicate(arr)
		require.Equal(t, want, got)
	})
}
