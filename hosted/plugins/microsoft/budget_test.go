package microsoft

import (
	"context"
	"testing"
)

func TestPollRequestBudgetBoundsAllPaginationKinds(t *testing.T) {
	ctx := withFetchBudget(context.Background())
	for i := 0; i < maximumFetchRequests; i++ {
		if err := claimFetchRequest(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := claimFetchRequest(ctx); err == nil {
		t.Fatal("request budget not enforced")
	}
	fresh := withFetchBudget(context.Background())
	if err := claimFetchRequest(fresh); err != nil {
		t.Fatal("independent fetch inherited exhausted budget")
	}
}
