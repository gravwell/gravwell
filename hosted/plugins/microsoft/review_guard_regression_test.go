package microsoft

import (
	"strings"
	"testing"
	"time"
)

func TestManagementNumericRetryAfterEnforcesMaximum(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	maximumSeconds := uint64(managementMaximumRetryAfter / time.Second)

	delay, err := managementRetryDelay("86400", now, time.Second)
	if err != nil || delay != managementMaximumRetryAfter || maximumSeconds != 86400 {
		t.Fatalf("maximum numeric Retry-After delay=%s maximum=%d error=%v", delay, maximumSeconds, err)
	}
	if _, err := managementRetryDelay("4000000000", now, time.Second); err == nil || !strings.Contains(err.Error(), "exceeds the supported bound") {
		t.Fatalf("in-range uint32 but over-bound Retry-After error=%v", err)
	}
}

// Every Management Activity array is now streamed under a type-specific count
// bound rather than decoded whole, so the strict trailing-data rejection is
// pinned on each of the three decoders instead of on one generic helper.
func TestManagementArrayRejectsTrailingJSON(t *testing.T) {
	const trailing = `[{"id":"one"}] {"unexpected":true}`
	if _, err := decodeManagementEvents([]byte(trailing), managementMaximumEventsPerContent); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("content trailing JSON error=%v", err)
	}
	if _, err := decodeManagementSubscriptions([]byte(trailing), managementMaximumSubscriptions); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("subscription trailing JSON error=%v", err)
	}
	if _, err := decodeManagementContentPage([]byte(trailing), managementMaximumDescriptors); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("content list trailing JSON error=%v", err)
	}
}

func TestRememberRecentMaintainsBoundedNewestRing(t *testing.T) {
	recent := make([]string, maximumContinuationHistory)
	for i := range recent {
		recent[i] = continuationFingerprint(string(rune('a' + i)))
	}
	oldestRetained := recent[1]
	newValue := "https://example.invalid/next"
	if err := rememberRecent(&recent, newValue); err != nil {
		t.Fatal(err)
	}
	if len(recent) != maximumContinuationHistory {
		t.Fatalf("history length=%d, want %d", len(recent), maximumContinuationHistory)
	}
	if recent[0] != oldestRetained {
		t.Fatalf("history did not evict exactly the oldest value")
	}
	if recent[len(recent)-1] != continuationFingerprint(newValue) {
		t.Fatalf("newest continuation was not retained")
	}
}
