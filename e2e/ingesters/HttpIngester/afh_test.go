package HttpIngester

import (
	"bytes"
	"encoding/json"
	"fmt"
	"gravwell/e2e"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

func TestAFH(t *testing.T) {
	_, endpoint := setup(t, "afh")

	t.Run("single data ingest", func(t *testing.T) {
		ts := time.Now()
		SendAFHRecords(t, endpoint, ts, []string{"ingest data"})

		c := e2e.GetClient(t)
		ents := e2e.WaitForEntries(t, c, `tag=afh`, time.Minute, 1, 30*time.Second)
		assert(t, ents, 1, "ingest data")
		if ents[0].TS.UnixMilli() != ts.UnixMilli() {
			t.Errorf("got TS %v, want %v", ents[0].TS, ts)
		}
	})

	t.Run("multiple data ingests", func(t *testing.T) {
		ts := time.Now()
		SendAFHRecords(t, endpoint, ts, []string{"ingest multiple", "ingest multiple"})

		c := e2e.GetClient(t)
		ents := e2e.WaitForEntries(t, c, `tag=afh words multiple`, time.Minute, 2, 30*time.Second)
		assert(t, ents, 2, "ingest multiple")
		if ents[0].TS.UnixMilli() != ts.UnixMilli() {
			t.Errorf("got TS %v, want %v", ents[0].TS, ts)
		}
	})

	t.Run("auth fails with bad token", func(t *testing.T) {
		req, err := http.NewRequest("POST", endpoint+"/amazon/ingest", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Amz-Firehose-Access-Key", "failure")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer utils.DrainResponse(resp)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})
}

type record struct {
	Data []byte `json:"data"`
}

type request struct {
	RequestId string   `json:"requestId"`
	Timestamp int64    `json:"timestamp"`
	Records   []record `json:"records"`
}

type response struct {
	RequestId string `json:"requestId"`
	Timestamp int64  `json:"timestamp"`
	Message   string `json:"errorMessage,omitempty"`
}

func SendAFHRecords(t *testing.T, endpoint string, ts time.Time, records []string) {
	t.Helper()

	rid := fmt.Sprintf("%d", rand.Int())
	data := request{
		RequestId: rid,
		Timestamp: ts.UnixMilli(),
		Records:   make([]record, 0, len(records)),
	}
	for _, r := range records {
		data.Records = append(data.Records, record{Data: []byte(r)})
	}
	body, err := json.Marshal(&data)
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", endpoint+"/amazon/ingest", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Amz-Firehose-Access-Key", "token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer utils.DrainResponse(resp)
	result := &response{}
	if err = json.NewDecoder(resp.Body).Decode(result); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d, err: %s", resp.StatusCode, http.StatusOK, result.Message)
	}
	if result.RequestId != rid {
		t.Fatalf("got requestId %s, want %s", result.RequestId, rid)
	}
}
