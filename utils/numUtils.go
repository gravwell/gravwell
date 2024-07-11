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
