/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package microsoft

import (
	"context"
	"errors"
	"time"
)

// Bounds apply to one selector/subscription chunk, including parent/child and
// multi-day pagination. Hitting a budget returns a durable continuation so the
// next immediate run resumes at the same page.
const maximumFetchRequests = 1000

var (
	errFetchBudget = errors.New("Microsoft fetch chunk reached its work budget")
	errFetchPacing = errors.New("Microsoft fetch chunk reached its pacing deadline")
)

type fetchBudgetKey struct{}
type fetchDeadlineKey struct{}
type fetchBudget struct {
	requestsRemaining int
}

func withFetchDeadline(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	owned := true
	if deadline, ok := parent.Deadline(); ok && time.Until(deadline) <= timeout {
		owned, _ = parent.Value(fetchDeadlineKey{}).(bool)
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	return context.WithValue(ctx, fetchDeadlineKey{}, owned), cancel
}

func internalFetchDeadline(ctx context.Context) bool {
	owned, _ := ctx.Value(fetchDeadlineKey{}).(bool)
	return owned
}

func deferredFetch(err error) bool {
	return errors.Is(err, errFetchBudget) || errors.Is(err, errFetchPacing)
}

// deferredFetchDeadline maps expiry of a deadline this plugin installed onto
// errFetchPacing, the yield every paginated fetch loop already turns into a
// durable continuation over the records it has collected so far.
//
// Rate-limiter exhaustion was the only way the fetch deadline reached that
// path, because it is reported before the deadline arrives. A request that
// crosses the same deadline in flight arrives in one of two other shapes --
// a dispatch failure wrapping context.DeadlineExceeded, or the last retryable
// response handed back with the fetch context already expired -- and
// deferredFetch matched neither. A slow multi-page or Advanced Hunting fetch
// therefore discarded every page it had completed and resumed from the last
// stored checkpoint on the next pass, which could repeat without ever
// checkpointing.
//
// Only a deadline withFetchDeadline owns is remapped, and only when it has
// actually expired. Caller cancellation keeps context.Canceled, a caller-owned
// deadline keeps context.DeadlineExceeded -- the caller is not asking this
// plugin to resume anything -- and a per-attempt transport timeout taken while
// the fetch context is still live stays the transport failure it is.
func deferredFetchDeadline(ctx context.Context, err error) error {
	if err == nil || !internalFetchDeadline(ctx) {
		return err
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) || !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errFetchPacing
}

func withFetchBudget(ctx context.Context) context.Context {
	return context.WithValue(ctx, fetchBudgetKey{}, &fetchBudget{maximumFetchRequests})
}

func claimFetchRequest(ctx context.Context) error {
	if budget, ok := ctx.Value(fetchBudgetKey{}).(*fetchBudget); ok {
		if budget.requestsRemaining <= 0 {
			return errFetchBudget
		}
		budget.requestsRemaining--
	}
	return nil
}

func canFetchAnotherPage(ctx context.Context) bool {
	budget, ok := ctx.Value(fetchBudgetKey{}).(*fetchBudget)
	return !ok || budget.requestsRemaining > 0
}

// managementFetchSpanKey carries the cumulative wall-clock budget one Management
// Activity pass may spend waiting on the source. The per-request fetch deadline
// alone is not enough once the entry-delivery loop no longer shares it: a
// pass may issue one request per content descriptor, so the span is what keeps
// one selector from holding the poll for deadline x descriptors. Exhausting it
// yields errFetchBudget, which every Management caller turns into a durable
// continuation, and the next pass starts with a fresh span. At most one request
// may overrun, so a pass spends at most one span plus one deadline in the
// source. Like fetchBudget it is only ever touched by the single goroutine that
// runs the pass.
type managementFetchSpanKey struct{}

type managementFetchSpanBudget struct {
	remaining time.Duration
}

func withManagementFetchSpan(parent context.Context, span time.Duration) context.Context {
	if span <= 0 {
		return parent
	}
	return context.WithValue(parent, managementFetchSpanKey{}, &managementFetchSpanBudget{remaining: span})
}

func claimManagementFetchSpan(ctx context.Context) error {
	if budget, ok := ctx.Value(managementFetchSpanKey{}).(*managementFetchSpanBudget); ok && budget.remaining <= 0 {
		return errFetchBudget
	}
	return nil
}

func chargeManagementFetchSpan(ctx context.Context, elapsed time.Duration) {
	if budget, ok := ctx.Value(managementFetchSpanKey{}).(*managementFetchSpanBudget); ok && elapsed > 0 {
		budget.remaining -= elapsed
	}
}
