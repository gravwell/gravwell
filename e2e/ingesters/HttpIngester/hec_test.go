package HttpIngester

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"gravwell/e2e"

	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestHec(t *testing.T) {
	_, endpoint := setup(t, "hec")

	t.Run("event ingest", func(t *testing.T) {
		data := `{"event": "hec blah"}`
		SendHecEvent(t, endpoint, strings.NewReader(data))

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, "tag=hec-testing words hec blah", time.Minute, 1, 30*time.Second), 1, "hec blah")
	})

	t.Run("raw ingest", func(t *testing.T) {
		data := `raw hec`
		SendHecRaw(t, endpoint, strings.NewReader(data))

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, "tag=hec-testing words raw hec", time.Minute, 1, 30*time.Second), 1, data)
	})

	t.Run("auth fails with bad token", func(t *testing.T) {
		req, err := http.NewRequest("POST", endpoint+"/services/collector/event", strings.NewReader(`{"event": "hec blah"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Splunk failure")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})

	t.Run("unexpected basic auth is accepted", func(t *testing.T) {
		// the "testing" listener is configured with a Splunk-style token name, but
		// clients sometimes throw HTTP Basic authentication at HEC endpoints instead;
		// the username is ignored and the password is checked against the token
		data := `{"event": "hec basic auth"}`
		req, err := http.NewRequest("POST", endpoint+"/services/collector/event", strings.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth("ignored-user", "token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer utils.DrainResponse(resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
		}

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, "tag=hec-testing words hec basic auth", time.Minute, 1, 30*time.Second), 1, "hec basic auth")
	})

	t.Run("unexpected basic auth fails with wrong password", func(t *testing.T) {
		req, err := http.NewRequest("POST", endpoint+"/services/collector/event", strings.NewReader(`{"event": "hec basic auth failure"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth("ignored-user", "wrong-password")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})

	t.Run("debug posts", func(t *testing.T) {
		data := `{"event": "hec blah blah"}`
		SendHecEvent(t, endpoint, strings.NewReader(data))

		c := e2e.GetClient(t)
		ent := e2e.WaitForEntries(t, c, fmt.Sprintf(`tag=gravwell syslog Appname==httpingester Message=="HEC request" bytes==%d`, len(data)), time.Minute, 1, 30*time.Second)
		if len(ent) != 1 {
			e2e.Fatalf(t, "got %d entries, want 1", len(ent))
		}
	})

	t.Run("implicit evs", func(t *testing.T) {
		data := `{"event": "hec ev", "host": "test-host", "source": "test-source", "fields": {"custom-field": "field-value"}}`
		SendHecEvent(t, endpoint, strings.NewReader(data))

		c := e2e.GetClient(t)
		ents := e2e.WaitForEntries(t, c, "tag=hec-testing intrinsic host==test-host", time.Minute, 1, 30*time.Second)
		assert(t, ents, 1, "hec ev")
		assertEV(t, ents[0], "source", "test-source")
		assertEV(t, ents[0], "custom-field", "field-value")
	})

	t.Run("Token-Name override to Basic enforces literal token, no basic auth", func(t *testing.T) {
		// the "basic-override" listener sets Token-Name="Basic", so the literal string
		// after "Basic " in the Authorization header must equal the configured token,
		// and real HTTP Basic authentication (base64 user:pass) must NOT be honored

		t.Run("literal token value succeeds", func(t *testing.T) {
			data := `{"event": "hec basic override literal"}`
			req, err := http.NewRequest("POST", endpoint+"/services/collector-basic-override/event", strings.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Basic basic-override-token")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer utils.DrainResponse(resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
			}

			c := e2e.GetClient(t)
			assert(t, e2e.WaitForEntries(t, c, "tag=hec-basic-override words hec basic override literal", time.Minute, 1, 30*time.Second), 1, "hec basic override literal")
		})

		t.Run("real HTTP basic auth is rejected", func(t *testing.T) {
			// this encodes to "Basic <base64(user:pass)>" which is NOT the literal
			// configured token, and since Token-Name is already "Basic" the
			// hecAuthHandler must not fall back to decoding it as HTTP Basic auth
			req, err := http.NewRequest("POST", endpoint+"/services/collector-basic-override/event", strings.NewReader(`{"event": "hec basic override rejected"}`))
			if err != nil {
				t.Fatal(err)
			}
			req.SetBasicAuth("ignored-user", "basic-override-token")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusUnauthorized)
			}
		})
	})
}

func assert(t *testing.T, ent []types.StringTagEntry, count int, data string) {
	t.Helper()
	if len(ent) != count {
		e2e.Fatalf(t, "got %d entries, want %d", len(ent), count)
	}
	for i, entry := range ent {
		if string(entry.Data) != data {
			e2e.Fatalf(t, "got %s, want %s, for entry %d", string(entry.Data), data, i)
		}
	}
}

func assertEV(t *testing.T, ent types.StringTagEntry, key string, value string) {
	t.Helper()
	ev, ok := ent.GetEnumerated(key)
	if !ok {
		e2e.Fatalf(t, "got no ev, want %q", key)
	}
	if ev != value {
		e2e.Fatalf(t, "for %q, got %q, want %q", key, ev, value)
	}
}

func TestHecNoDebug(t *testing.T) {
	_, endpoint := setup(t, "hec-no-debug",
		// we want to run without -v to ensure basics work
		tc.WithCmd("/opt/gravwell/bin/HttpIngester"),
		// since we run without -v we need to remove the wait strategy that relies on the verbose output
		tc.WithWaitStrategyAndDeadline(10*time.Second, wait.NewHTTPStrategy("/health/check").WithPollInterval(time.Second)),
	)

	t.Run("event ingest", func(t *testing.T) {
		data := `{"event": "hec no debug"}`
		SendHecEvent(t, endpoint, strings.NewReader(data))

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, "tag=hec-no-debug words hec no debug", time.Minute, 1, 30*time.Second), 1, "hec no debug")
	})

	t.Run("raw ingest", func(t *testing.T) {
		data := `raw hec no debug`
		SendHecRaw(t, endpoint, strings.NewReader(data))

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, "tag=hec-no-debug words raw hec no debug", time.Minute, 1, 30*time.Second), 1, data)
	})
}

func SendHecEvent(t *testing.T, endpoint string, data io.Reader) {
	sendHec(t, endpoint+"/services/collector/event", data)
}

func SendHecRaw(t *testing.T, endpoint string, data io.Reader) {
	sendHec(t, endpoint+"/services/collector/raw", data)
}

func sendHec(t *testing.T, endpoint string, data io.Reader) {
	req, err := http.NewRequest("POST", endpoint, data)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Splunk token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer utils.DrainResponse(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got bad http status %d", resp.StatusCode)
	}
}
