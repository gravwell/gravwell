/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"errors"
	"testing"
)

func TestQueryOptionsValidateFilterCaps(t *testing.T) {
	filters := func(n, values int) []Filter {
		fs := make([]Filter, n)
		for i := range fs {
			fs[i] = Filter{Key: "Name", Operation: "=", Values: make([]any, values)}
		}
		return fs
	}
	tests := []struct {
		name    string
		filters []Filter
		want    error
	}{
		{"AtCaps", filters(QueryOptionsMaxFilters, QueryOptionsMaxFilterValues), nil},
		{"TooManyFilters", filters(QueryOptionsMaxFilters+1, 1), ErrTooManyFilters},
		{"TooManyValues", filters(1, QueryOptionsMaxFilterValues+1), ErrTooManyFilterValues},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qo := DefaultQueryOptions()
			qo.Filters = tt.filters
			if err := qo.Validate(); !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
		})
	}
}
