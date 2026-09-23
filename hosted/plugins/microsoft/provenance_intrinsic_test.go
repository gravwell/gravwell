package microsoft

import (
	"bytes"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestAttachIntrinsicProvenancePreservesData(t *testing.T) {
	raw := []byte("{\"native\":true}\n")
	ent := entry.Entry{Data: append([]byte(nil), raw...)}
	metadata := map[string]string{
		"_vendor": "Example Vendor", "_product": "Example Product", "_source": "audit-events",
		"_recordType": "auditEvent", "_endpoint": "GET /api/v1/audit/events", "_apiVersion": "v1",
	}
	if err := attachIntrinsic(&ent, metadata); err != nil {
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
	err := attachIntrinsic(&entry.Entry{}, map[string]string{
		"_vendor": "Example Vendor", "_product": "Example Product", "_source": "audit-events",
		"_recordType": "auditEvent", "_endpoint": "GET /api/v1/audit/events",
	})
	if err == nil {
		t.Fatal("expected incomplete metadata to fail")
	}
}

func TestAttachIntrinsicProvenanceRejectsEndpointLineBreak(t *testing.T) {
	err := attachIntrinsic(&entry.Entry{}, map[string]string{
		"_vendor": "Example Vendor", "_product": "Example Product", "_source": "audit-events",
		"_recordType": "auditEvent", "_endpoint": "GET /api/v1/audit/events\nforged", "_apiVersion": "v1",
	})
	if err == nil {
		t.Fatal("expected endpoint line break to fail")
	}
}
