package servicenow

import (
	"bytes"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestAttachIntrinsicProvenancePreservesData(t *testing.T) {
	raw := []byte("{\"native\":true}\n")
	ent := entry.Entry{Data: append([]byte(nil), raw...)}
	metadata := provenanceMetadata{
		Vendor: "Example Vendor", Product: "Example Product", Source: "audit-events",
		RecordType: "auditEvent", Endpoint: "GET /api/v1/audit/events", APIVersion: "v1",
	}
	if err := attachIntrinsicProvenance(&ent, metadata); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ent.Data, raw) {
		t.Fatalf("entry data changed: got %q want %q", ent.Data, raw)
	}
	for _, name := range []string{"_vendor", "_product", "_source", "_recordType", "_endpoint", "_apiVersion"} {
		if _, ok := ent.GetEnumeratedValue(name); !ok {
			t.Fatalf("missing intrinsic value %s", name)
		}
	}
}

func TestAttachIntrinsicProvenanceRejectsIncompleteMetadata(t *testing.T) {
	err := attachIntrinsicProvenance(&entry.Entry{}, provenanceMetadata{
		Vendor: "Example Vendor", Product: "Example Product", Source: "audit-events",
		RecordType: "auditEvent", Endpoint: "GET /api/v1/audit/events",
	})
	if err == nil {
		t.Fatal("expected incomplete metadata to fail")
	}
}

func TestAttachIntrinsicProvenanceRejectsEndpointLineBreak(t *testing.T) {
	err := attachIntrinsicProvenance(&entry.Entry{}, provenanceMetadata{
		Vendor: "Example Vendor", Product: "Example Product", Source: "audit-events",
		RecordType: "auditEvent", Endpoint: "GET /api/v1/audit/events\nforged", APIVersion: "v1",
	})
	if err == nil {
		t.Fatal("expected endpoint line break to fail")
	}
}

func TestAttachNormalizationIntrinsicPreservesData(t *testing.T) {
	raw := []byte(`{"userName":"canonical","user_name":"variant"}`)
	ent := entry.Entry{Data: append([]byte(nil), raw...)}
	if err := AttachNormalizationIntrinsic(&ent, map[string]string{
		"_normalizationCollision": "userName(userName|user_name)",
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ent.Data, raw) {
		t.Fatalf("entry data changed: got %q want %q", ent.Data, raw)
	}
	value, ok := ent.GetEnumeratedValue("_normalizationCollision")
	encoded, typed := value.([]byte)
	if !ok || !typed || string(encoded) != "userName(userName|user_name)" {
		t.Fatalf("collision intrinsic=%q present=%v", value, ok)
	}
	if err := AttachNormalizationIntrinsic(&entry.Entry{}, map[string]string{"_unexpected": "value"}); err == nil {
		t.Fatal("unsupported normalization intrinsic was accepted")
	}
}
