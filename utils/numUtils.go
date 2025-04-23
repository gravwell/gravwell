/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package utils provides various helpers that don't belong anywhere else
package utils

import (
	"slices"
)

func Int32SlicesEqual(a, b []int32) bool {
	A := make([]int32, len(a))
	B := make([]int32, len(b))
	copy(A, a)
	copy(B, b)
	slices.Sort(A)
	slices.Sort(B)
	return slices.Compare(A, B) == 0
}
