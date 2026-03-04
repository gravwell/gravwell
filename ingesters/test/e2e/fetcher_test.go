package e2e

import (
	"net/http"
	"testing"
)

func TestTestPlugin(t testing.T) {
	endpoint, err := gravwell.PortEndpoint(t.Context(), "80", "")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.Status)
	}
}
