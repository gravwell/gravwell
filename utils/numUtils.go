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

// Deduplicate returns arr with all duplicate eliminated.
// Order is maintained according to first appearance of an item.
func Deduplicate[T comparable](arr []T) []T {
	dd := make(map[T]bool, len(arr)) // value -> order
	new := make([]T, 0, len(arr))
	for _, a := range arr {
		if _, found := dd[a]; !found {
			new = append(new, a)
			dd[a] = true
		}
	}
	return slices.Clip(new)
}
