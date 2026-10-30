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

func TestResourceGraphTruncationWithoutContinuationIsRejected(t *testing.T) {
	for _, truncated := range []string{`true`, `"true"`} {
		t.Run(truncated, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/token") {
					fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
					return
				}
				fmt.Fprintf(w, `{"data":[{"id":"synthetic-resource","type":"Microsoft.Compute/virtualMachines"}],"resultTruncated":%s}`, truncated)
			}))
			defer server.Close()
			client, err := NewClient(testClientConfig(t, server.URL), nil)
			if err != nil {
				t.Fatal(err)
			}
			client.http.Transport = server.Client().Transport
			if _, err := client.Fetch(context.Background(), datasets["azure-resource-inventory"], time.Now(), time.Now(), ""); err == nil {
				t.Fatal("truncated inventory accepted without continuation")
			}
		})
	}
}
