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

func TestFabricSubMillisecondMidnightBoundary(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		start, e1 := time.Parse(time.RFC3339Nano, strings.Trim(r.URL.Query().Get("startDateTime"), "'"))
		end, e2 := time.Parse(time.RFC3339Nano, strings.Trim(r.URL.Query().Get("endDateTime"), "'"))
		if e1 != nil || e2 != nil || start.After(end) || start.Day() != end.Day() {
			t.Errorf("invalid Fabric day window start=%s end=%s", start, end)
		}
		fmt.Fprint(w, `{"activityEventEntities":[]}`)
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = server.Client().Transport
	start := time.Date(2026, 9, 20, 23, 59, 59, 999500000, time.UTC)
	if _, err := client.Fetch(context.Background(), datasets["fabric-activity"], start, start.Add(time.Second), ""); err != nil {
		t.Fatal(err)
	}
}
