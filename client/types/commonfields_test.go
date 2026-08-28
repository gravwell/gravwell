/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestNullableTimeMarshalJSON pins NullableTime's own null-when-zero
// contract in isolation from CommonFields/SearchInfo, since it's the type
// that actually carries the wire-format logic.
func TestNullableTimeMarshalJSON(t *testing.T) {
	if b, err := json.Marshal(NullableTime{}); err != nil {
		t.Fatalf("marshal zero value: %v", err)
	} else if string(b) != "null" {
		t.Errorf("expected zero value to marshal to null, got %s", b)
	}

	ts := time.Date(2026, 7, 8, 9, 10, 11, 0, time.UTC)
	if b, err := json.Marshal(NullableTime(ts)); err != nil {
		t.Fatalf("marshal non-zero value: %v", err)
	} else if want := `"` + ts.Format(time.RFC3339Nano) + `"`; string(b) != want {
		t.Errorf("got %s want %s", b, want)
	}
}

// TestNullableTimeUnmarshalJSON pins the decode side: null maps to the zero
// value, and a real date-time string decodes to the equivalent instant.
func TestNullableTimeUnmarshalJSON(t *testing.T) {
	var nt NullableTime
	if err := json.Unmarshal([]byte("null"), &nt); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if !nt.IsZero() {
		t.Errorf("expected zero value after unmarshaling null, got %v", nt.Time())
	}

	ts := time.Date(2026, 7, 8, 9, 10, 11, 0, time.UTC)
	if err := json.Unmarshal([]byte(`"`+ts.Format(time.RFC3339Nano)+`"`), &nt); err != nil {
		t.Fatalf("unmarshal timestamp: %v", err)
	}
	if !nt.Time().Equal(ts) {
		t.Errorf("got %v want %v", nt.Time(), ts)
	}
}

// TestNullableTimeHelpers exercises Time/IsZero/Equal directly, since these
// are what call sites throughout the codebase (registry conversions, gwcli
// display code, etc.) use in place of the plain time.Time methods they
// replace.
func TestNullableTimeHelpers(t *testing.T) {
	if !(NullableTime{}).IsZero() {
		t.Error("zero value should report IsZero true")
	}
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nt := NullableTime(ts)
	if nt.IsZero() {
		t.Error("non-zero value should report IsZero false")
	}
	if !nt.Time().Equal(ts) {
		t.Errorf("Time() got %v want %v", nt.Time(), ts)
	}
	if !nt.Equal(NullableTime(ts)) {
		t.Error("Equal should report true for the same instant")
	}
	if nt.Equal(NullableTime(ts.Add(time.Second))) {
		t.Error("Equal should report false for different instants")
	}
}

// TestCommonFieldsZeroTimestampsMarshalNull pins the wire contract behind
// gravwell/issues#2790: UpdatedAt/DeletedAt must encode as JSON null when
// unset (matching the API spec's nullable AssetCommonFields schema), while
// CreatedAt is required/non-nullable and always encodes as a real
// date-time string, even when it happens to be the Go zero value.
func TestCommonFieldsZeroTimestampsMarshalNull(t *testing.T) {
	cf := CommonFields{ID: "zero-ts"}
	b, err := json.Marshal(cf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"UpdatedAt":null`) {
		t.Errorf("expected UpdatedAt:null, got: %s", s)
	}
	if !strings.Contains(s, `"DeletedAt":null`) {
		t.Errorf("expected DeletedAt:null, got: %s", s)
	}
	if strings.Contains(s, `"CreatedAt":null`) {
		t.Errorf("CreatedAt must never be null: %s", s)
	}
	if !strings.Contains(s, `"CreatedAt":"0001-01-01T00:00:00Z"`) {
		t.Errorf("expected zero-value CreatedAt to still encode as a real timestamp, got: %s", s)
	}
}

// TestCommonFieldsNonZeroTimestampsMarshalValue ensures a populated
// UpdatedAt/DeletedAt is not accidentally nulled out by the null-when-zero
// logic.
func TestCommonFieldsNonZeroTimestampsMarshalValue(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	updated := created.Add(time.Hour)
	deleted := created.Add(2 * time.Hour)
	cf := CommonFields{ID: "non-zero-ts", CreatedAt: created, UpdatedAt: NullableTime(updated), DeletedAt: NullableTime(deleted)}

	b, err := json.Marshal(cf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"CreatedAt":"` + created.Format(time.RFC3339Nano) + `"`,
		`"UpdatedAt":"` + updated.Format(time.RFC3339Nano) + `"`,
		`"DeletedAt":"` + deleted.Format(time.RFC3339Nano) + `"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %s in output, got: %s", want, s)
		}
	}
}

// TestCommonFieldsUnmarshalNullToZeroTime confirms that a JSON null (or an
// absent key entirely) for UpdatedAt/DeletedAt decodes back to the Go zero
// time.Time, so existing .IsZero() call sites throughout the codebase (and
// internal logic like the registry's "deleted_at == 0" predicate) keep
// working unchanged after this wire-format change.
func TestCommonFieldsUnmarshalNullToZeroTime(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{name: "explicit null", in: `{"ID":"x","CreatedAt":"2026-01-02T03:04:05Z","UpdatedAt":null,"DeletedAt":null}`},
		{name: "keys absent", in: `{"ID":"x","CreatedAt":"2026-01-02T03:04:05Z"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cf CommonFields
			if err := json.Unmarshal([]byte(tt.in), &cf); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !cf.UpdatedAt.IsZero() {
				t.Errorf("expected zero UpdatedAt, got %v", cf.UpdatedAt)
			}
			if !cf.DeletedAt.IsZero() {
				t.Errorf("expected zero DeletedAt, got %v", cf.DeletedAt)
			}
			if cf.CreatedAt.IsZero() {
				t.Errorf("CreatedAt should have decoded to a real value")
			}
		})
	}
}

// TestCommonFieldsMarshalUnmarshalRoundTrip exercises the full
// Marshal->Unmarshal cycle and requires every other field survive alongside
// the custom timestamp handling, guarding against the shadow "alias" struct
// silently dropping fields.
func TestCommonFieldsMarshalUnmarshalRoundTrip(t *testing.T) {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	updated := created.Add(time.Minute)
	orig := CommonFields{
		Type:        AssetDashboard,
		CreatedAt:   created,
		UpdatedAt:   NullableTime(updated),
		DeletedAt:   NullableTime(time.Time{}),
		ID:          "id-1",
		ParentID:    "parent-1",
		OwnerID:     42,
		Name:        "my dashboard",
		Description: "a description",
		Labels:      []string{"a", "b"},
		Version:     3,
		Kit:         "kit-1",
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got CommonFields
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !got.CreatedAt.Equal(orig.CreatedAt) {
		t.Errorf("CreatedAt mismatch: got %v want %v", got.CreatedAt, orig.CreatedAt)
	}
	if !got.UpdatedAt.Equal(orig.UpdatedAt) {
		t.Errorf("UpdatedAt mismatch: got %v want %v", got.UpdatedAt, orig.UpdatedAt)
	}
	if !got.DeletedAt.IsZero() {
		t.Errorf("DeletedAt mismatch: expected zero, got %v", got.DeletedAt)
	}
	if got.Type != orig.Type || got.ID != orig.ID || got.ParentID != orig.ParentID ||
		got.OwnerID != orig.OwnerID || got.Name != orig.Name || got.Description != orig.Description ||
		got.Version != orig.Version || got.Kit != orig.Kit {
		t.Errorf("non-timestamp fields did not round-trip: got %+v want %+v", got, orig)
	}
	if len(got.Labels) != len(orig.Labels) {
		t.Errorf("Labels did not round-trip: got %v want %v", got.Labels, orig.Labels)
	}
}

// TestSearchInfoEmbeddedCommonFieldsNullBehavior confirms the null-when-zero
// contract survives Go's anonymous-embedding promotion when CommonFields is
// embedded in a real asset type, not just exercised in isolation. This is
// the shape an actual API response takes (e.g. GET /api/searchctrl/{id}),
// and is what would have masked the null behavior if promotion didn't work.
func TestSearchInfoEmbeddedCommonFieldsNullBehavior(t *testing.T) {
	created := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	si := SearchInfo{
		CommonFields: CommonFields{ID: "search-1", CreatedAt: created},
		UserQuery:    "tag=default",
	}

	b, err := json.Marshal(si)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"UpdatedAt":null`) {
		t.Errorf("expected UpdatedAt:null on embedded CommonFields, got: %s", s)
	}
	if !strings.Contains(s, `"DeletedAt":null`) {
		t.Errorf("expected DeletedAt:null on embedded CommonFields, got: %s", s)
	}
	if !strings.Contains(s, `"CreatedAt":"`+created.Format(time.RFC3339Nano)+`"`) {
		t.Errorf("expected populated CreatedAt on embedded CommonFields, got: %s", s)
	}
	if !strings.Contains(s, `"UserQuery":"tag=default"`) {
		t.Errorf("SearchInfo's own fields must still be present alongside embedded CommonFields, got: %s", s)
	}

	var got SearchInfo
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt mismatch after round trip: got %v want %v", got.CreatedAt, created)
	}
	if !got.UpdatedAt.IsZero() || !got.DeletedAt.IsZero() {
		t.Errorf("expected zero UpdatedAt/DeletedAt after round trip, got %v / %v", got.UpdatedAt, got.DeletedAt)
	}
	if got.UserQuery != si.UserQuery {
		t.Errorf("UserQuery mismatch after round trip: got %q want %q", got.UserQuery, si.UserQuery)
	}
}

// TestSearchInfoSliceWithMixedTimestampsMarshals mirrors
// TestSearchInfoWithEmptyRendererSettings's concern: a MarshalJSON error on
// any one element propagates to the top of the enclosing document and
// blanks the whole slice. This guards that a mix of zero and populated
// CommonFields timestamps across multiple SearchInfo values never trips
// that failure mode.
func TestSearchInfoSliceWithMixedTimestampsMarshals(t *testing.T) {
	si := []SearchInfo{
		{CommonFields: CommonFields{ID: "a"}},
		{CommonFields: CommonFields{ID: "b", UpdatedAt: NullableTime(time.Now()), DeletedAt: NullableTime(time.Now())}},
		{CommonFields: CommonFields{ID: "c"}},
	}
	b, err := json.Marshal(si)
	if err != nil {
		t.Fatalf("SearchInfo slice with mixed timestamps must marshal: %v", err)
	}
	for _, id := range []string{`"a"`, `"b"`, `"c"`} {
		if !strings.Contains(string(b), id) {
			t.Errorf("SearchInfo %s missing from output: %s", id, b)
		}
	}
}
