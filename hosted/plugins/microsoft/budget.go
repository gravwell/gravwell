package microsoft

import (
	"context"
	"errors"
)

// Bounds apply to one selector/subscription poll, including parent/child and
// multi-day pagination. Hitting a budget fails the complete poll, so no partial
// response can advance its checkpoint. Bodies are still capped individually.
const maximumFetchBytes int64 = 128 << 20
const maximumFetchRequests = 1000

type fetchBudgetKey struct{}
type fetchBudget struct {
	bytesRemaining    int64
	requestsRemaining int
}

func withFetchBudget(ctx context.Context) context.Context {
	return context.WithValue(ctx, fetchBudgetKey{}, &fetchBudget{maximumFetchBytes, maximumFetchRequests})
}

func claimFetchRequest(ctx context.Context) error {
	if budget, ok := ctx.Value(fetchBudgetKey{}).(*fetchBudget); ok {
		if budget.requestsRemaining <= 0 {
			return errors.New("Microsoft poll exceeded its total request budget; checkpoint retained")
		}
		budget.requestsRemaining--
	}
	return nil
}

func responseLimit(ctx context.Context) int64 {
	limit := int64(maximumResponseBody)
	if budget, ok := ctx.Value(fetchBudgetKey{}).(*fetchBudget); ok && budget.bytesRemaining < limit {
		limit = budget.bytesRemaining
	}
	return limit
}

func chargeResponse(ctx context.Context, size int64) {
	if budget, ok := ctx.Value(fetchBudgetKey{}).(*fetchBudget); ok {
		budget.bytesRemaining -= size
	}
}
