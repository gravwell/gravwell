/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"errors"
	"iter"

	"github.com/gravwell/gravwell/v4/client/types"
)

// ErrCursorStalled is returned when a listing hands back the cursor it was just given,
// which would otherwise page forever.
var ErrCursorStalled = errors.New("listing returned the same cursor it was given")

type pager interface {
	GetNextCursor() string
}

// Lister is a single page of a listing, as returned by the list calls (ex: ListMacros).
type Lister[T any] interface {
	pager
	GetTotalCount() int
	Items() []T
}

// Pages iterates each page of a list call, walking the cursor returned in the result.
// opts is passed to the first call unaltered; later calls only change CursorID.
//
// Iteration stops when an error is encountered or CursorNext is empty.
func Pages[R pager](opts types.QueryOptions, list func(types.QueryOptions) (R, error)) iter.Seq2[R, error] {
	return func(yield func(R, error) bool) {
		for {
			page, err := list(opts)
			if !yield(page, err) || err != nil {
				return
			}
			next := page.GetNextCursor()
			switch next {
			case "":
				return
			case opts.CursorID:
				var zero R
				yield(zero, ErrCursorStalled)
				return
			}
			opts.CursorID = next
		}
	}
}

// Collect gathers the Results of list until:
// 1) an error is encountered,
// 2) all results have been collected, or
// 3) maxCount has been met/exceeded (if < 1, fetches all results).
//
// If opts.Limit is unset, the page size is min(QueryOptionsMaxLimit, maxCount > 0)
func Collect[R Lister[T], T any](opts types.QueryOptions, list func(types.QueryOptions) (R, error), maxCount int) ([]T, error) {
	if opts.Limit == 0 {
		opts.Limit = types.QueryOptionsMaxLimit
		if maxCount > 0 {
			opts.Limit = int32(min(maxCount, types.QueryOptionsMaxLimit))
		}
	}

	items := []T{}
	for page, err := range Pages(opts, list) {
		if err != nil {
			return items, err
		}
		results := page.Items()
		if maxCount > 0 { // only take what we still need, so we never grow past maxCount
			results = results[:min(len(results), maxCount-len(items))]
		}
		items = append(items, results...)
		if maxCount > 0 && len(items) >= maxCount {
			return items, nil
		}
	}
	return items, nil
}
