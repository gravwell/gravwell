package microsoft

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestNormalizationReconcilesFieldsWithoutChangingVendorRecord(t *testing.T) {
	raw := []byte(`{"id":"sign-in-1","createdDateTime":"2026-09-21T10:00:00Z","userPrincipalName":"synthetic@example.invalid","ipAddress":"192.0.2.7","large":9007199254740993}`)
	got, _, err := formatRecord(raw, "sign-in-1", time.Unix(1, 0), datasets["entra-signins"], "enabled")
	if err != nil {
		t.Fatal(err)
	}
	var normalized map[string]json.RawMessage
	if err := json.Unmarshal(got, &normalized); err != nil {
		t.Fatal(err)
	}
	var fields map[string]string
	if err := json.Unmarshal(normalized["canonical"], &fields); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"recordId": "sign-in-1", "recordTimestamp": "2026-09-21T10:00:00Z", "userName": "synthetic@example.invalid", "sourceIp": "192.0.2.7"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("canonical=%v want=%v", fields, want)
	}
	var before, after any
	a := json.NewDecoder(bytes.NewReader(raw))
	a.UseNumber()
	b := json.NewDecoder(bytes.NewReader(normalized["record"]))
	b.UseNumber()
	if a.Decode(&before) != nil || b.Decode(&after) != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("vendor fields changed")
	}
	if bytes.ContainsAny(got, "\r\n") {
		t.Fatal("normalization violated compact framing")
	}
	plain, _, err := formatRecord(raw, "sign-in-1", time.Unix(1, 0), datasets["entra-signins"], "disabled")
	if err != nil || !bytes.Equal(plain, raw) {
		t.Fatal("raw mode changed vendor bytes")
	}
}

func TestNormalizationCanonicalPrecedenceAndCollisions(t *testing.T) {
	raw := []byte(`{"id":"audit-1","userName":"existing@example.invalid","initiatedBy":{"user":{"userPrincipalName":"different@example.invalid"}},"activityDisplayName":"Add member to group","targetResources":[{"userPrincipalName":"target@example.invalid"}]}`)
	got, _, err := formatRecord(raw, "audit-1", time.Unix(1, 0), datasets["entra-directory-audits"], "true")
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Canonical  map[string]string `json:"canonical"`
		Collisions []struct {
			Field            string   `json:"field"`
			SelectedPath     string   `json:"selectedPath"`
			ConflictingPaths []string `json:"conflictingPaths"`
		} `json:"normalizationCollisions"`
	}
	if err := json.Unmarshal(got, &value); err != nil {
		t.Fatal(err)
	}
	if value.Canonical["userName"] != "existing@example.invalid" || value.Canonical["actionName"] != "Add member to group" {
		t.Fatalf("canonical=%v", value.Canonical)
	}
	if len(value.Collisions) != 1 || value.Collisions[0].Field != "userName" || value.Collisions[0].SelectedPath != "userName" || !reflect.DeepEqual(value.Collisions[0].ConflictingPaths, []string{"initiatedBy.user.userPrincipalName"}) {
		t.Fatalf("collisions=%+v", value.Collisions)
	}
}
