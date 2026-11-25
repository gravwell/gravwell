package microsoft

import (
	"encoding/json"
	"testing"
)

func TestEquivalentSelectorsCollapseWithoutMergingDistinctTags(t *testing.T) {
	resolved, err := ResolveDatasets([]string{"defender-endpoint-vulnerabilities", "defender-vulnerabilities", "defender-xdr-vulnerability"})
	if err != nil || len(resolved) != 2 {
		t.Fatalf("resolved=%v err=%v", resolved, err)
	}
	a := datasets["defender-endpoint-vulnerabilities"]
	b := a
	b.TagGroup = "different-consumer"
	if CollectionContractKey(a) == CollectionContractKey(b) {
		t.Fatal("distinct tags merged")
	}
	b = a
	b.Filter = "severity eq 'High'"
	if CollectionContractKey(a) == CollectionContractKey(b) {
		t.Fatal("distinct filters merged")
	}
	b = a
	b.ActivityCategory = "Security"
	if CollectionContractKey(a) == CollectionContractKey(b) {
		t.Fatal("distinct local activity categories merged")
	}
}

func TestVulnerabilitySnapshotPreservesMultipleCVEsOnOneDevice(t *testing.T) {
	d := datasets["defender-xdr-vulnerability"]
	ids := map[string]bool{}
	for _, cve := range []string{"CVE-DEMO-1", "CVE-DEMO-2"} {
		r, err := decodeRecord(d, json.RawMessage(`{"DeviceId":"device-1","SoftwareVendor":"vendor","SoftwareName":"app","SoftwareVersion":"1","CveId":"`+cve+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		if ids[r.ID] {
			t.Fatal("different CVEs collided")
		}
		ids[r.ID] = true
	}
}

func TestPartialCompositeIdentityFallsBackToPayloadHash(t *testing.T) {
	d := datasets["defender-xdr-alert-evidence"]
	a, err := decodeRecord(d, json.RawMessage(`{"Timestamp":"2026-09-08T14:00:00Z","DeviceId":"device-1","EntityType":"File","FileName":"a.exe"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := decodeRecord(d, json.RawMessage(`{"Timestamp":"2026-09-08T14:00:00Z","DeviceId":"device-1","EntityType":"File","FileName":"b.exe"}`))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("partial identities collapsed")
	}
}
