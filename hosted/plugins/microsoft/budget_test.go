package microsoft

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPollBudgetBoundsAllPagesAndParentChildren(t *testing.T) {
	ctx := withFetchBudget(context.Background())
	for i := 0; i < maximumFetchRequests; i++ {
		if err := claimFetchRequest(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := claimFetchRequest(ctx); err == nil {
		t.Fatal("request budget not enforced")
	}
	if got := responseLimit(ctx); got != maximumResponseBody {
		t.Fatalf("initial body limit=%d", got)
	}
	chargeResponse(ctx, maximumFetchBytes-17)
	if got := responseLimit(ctx); got != 17 {
		t.Fatalf("remaining body limit=%d", got)
	}
	fresh := withFetchBudget(context.Background())
	if err := claimFetchRequest(fresh); err != nil || responseLimit(fresh) != maximumResponseBody {
		t.Fatal("independent poll inherited exhausted budget")
	}
}

func TestProductionClientRejectsOversizedAPIResponse(t *testing.T) {
	chunk := []byte(strings.Repeat("x", 1<<20))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		fmt.Fprint(w, `{"value":[{"id":"synthetic","payload":"`)
		for i := 0; i < 65; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
		fmt.Fprint(w, `"}]}`)
	}))
	defer server.Close()
	c, err := NewClient(testClientConfig(t, server.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = server.Client().Transport
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := c.Fetch(ctx, datasets["entra-signins"], time.Now(), time.Now(), ""); err == nil || !strings.Contains(err.Error(), "byte budget") {
		t.Fatalf("response limit error=%v", err)
	}
}
