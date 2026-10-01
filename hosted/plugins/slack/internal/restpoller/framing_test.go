package restpoller

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestNormalizeRecordProducesOneCompactVendorJSONEntry(t *testing.T) {
	raw := json.RawMessage("{\n  \"id\": \"evt-1\",\n  \"effective_at\": \"2026-09-01T12:34:56Z\",\n  \"nested\": {\"value\": true}\n}")
	compact, identity, timestamp, err := normalizeRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.ContainsAny(compact, "\r\n") {
		t.Fatalf("record contains physical newline: %q", compact)
	}
	if !json.Valid(compact) || identity != "id:evt-1" {
		t.Fatalf("compact=%q identity=%q", compact, identity)
	}
	want, _ := time.Parse(time.RFC3339, "2026-09-01T12:34:56Z")
	if !timestamp.Equal(want) {
		t.Fatalf("timestamp=%s want=%s", timestamp, want)
	}
}
