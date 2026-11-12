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
	"testing"
)

func TestActionableTriggerUnmarshalObject(t *testing.T) {
	data := []byte(`{"Pattern":"test.*","ActivatesOn":"selection","Disabled":true}`)
	var trigger ActionableTrigger
	if err := json.Unmarshal(data, &trigger); err != nil {
		t.Fatal(err)
	}
	if trigger.Pattern != "test.*" {
		t.Fatalf("unexpected pattern: %q", trigger.Pattern)
	}
	if trigger.ActivatesOn != "selection" {
		t.Fatalf("expected ActivatesOn=selection, got %q", trigger.ActivatesOn)
	}
	if !trigger.Disabled {
		t.Fatal("expected Disabled=true")
	}
}

func TestActionableTriggerMarshal(t *testing.T) {
	trigger := ActionableTrigger{Pattern: "foo", ActivatesOn: "always", Disabled: false}
	data, err := json.Marshal(trigger)
	if err != nil {
		t.Fatal(err)
	}
	// Always marshals as object form.
	expected := `{"Pattern":"foo","ActivatesOn":"always","Disabled":false}`
	if string(data) != expected {
		t.Fatalf("expected %s, got %s", expected, string(data))
	}
}

func TestActionableContentRoundTrip(t *testing.T) {
	input := ActionableContent{
		MenuLabel: "My Pivot",
		Triggers: []ActionableTrigger{
			{Pattern: `\d+\.\d+\.\d+\.\d+`, ActivatesOn: "always", Disabled: false},
		},
		Actions: []ActionableAction{
			{
				Type:               ACTIONABLE_COMMAND_QUERY,
				Name:               "Query IP",
				Description:        "Look up IP",
				Query:              "tag=netflow src==_VALUE_",
				TriggerPlaceholder: "_VALUE_",
			},
			{
				Type:        ACTIONABLE_COMMAND_DASHBOARD,
				Name:        "Open Dashboard",
				DashboardID: "some-uuid",
				Variable:    "ip",
			},
			{
				Type:               ACTIONABLE_COMMAND_URL,
				Name:               "Open URL",
				TemplateURL:        "https://example.com/lookup?ip=_VALUE_",
				TriggerPlaceholder: "_VALUE_",
				OpenInModal:        true,
				ModalWidthPercent:  80,
				NoValueUrlEncode:   true,
				Start: &ActionableTimeVariable{
					Type:   "string",
					Format: "YYYY-MM-DD",
				},
				End: &ActionableTimeVariable{
					Type: "unix",
				},
			},
		},
	}

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var output ActionableContent
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Re-marshal and compare.
	data2, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(data) != string(data2) {
		t.Fatalf("round-trip mismatch:\n  got:  %s\n  want: %s", string(data2), string(data))
	}
}

// TestActionableTriggerListOptionsFields is the reason this type exists rather
// than reusing QueryOptions. QueryOptions has no omitzero tags, so marshalling
// even a zero value writes Type, All and Version, and the deduplicated trigger
// listing rejects all three: it has no owner to widen past and no history to
// pin, and taking them only to drop them would answer an admin's "show me
// everyone's" with their own and say nothing about it.
//
// So a caller handing this type to that endpoint has to come out with exactly
// the fields the published request body has, and no others.
func TestActionableTriggerListOptionsFields(t *testing.T) {
	raw, err := json.Marshal(ActionableTriggerListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"CursorID": true, "Limit": true, "Filters": true,
		"IncludeDeleted": true, "OrderBy": true, "OrderDirection": true, "Search": true,
	}
	for field := range got {
		if !want[field] {
			t.Errorf("marshalled an unexpected field %q; the trigger listing rejects anything outside its request body", field)
		}
	}
	for field := range want {
		if _, ok := got[field]; !ok {
			t.Errorf("did not marshal %q", field)
		}
	}
	for _, forbidden := range []string{"Type", "All", "Version"} {
		if _, ok := got[forbidden]; ok {
			t.Errorf("marshalled %q, which the endpoint answers with a 400", forbidden)
		}
	}
}

// TestActionableTriggerListResponseRoundTrip decodes a response in the shape
// the server sends, so that a change to either side shows up here.
func TestActionableTriggerListResponseRoundTrip(t *testing.T) {
	data := []byte(`{
		"CursorNext": "abc", "CursorPrev": "", "Offset": 0, "TotalCount": 2,
		"Type": "actionable_trigger", "AvailableFilters": [],
		"Results": [{
			"Pattern": "(a+)+",
			"ActivatesOn": "always",
			"Complexity": {"Severity": "high", "Backtracking": false, "Reasons": ["because"]},
			"Menus": [{
				"MenuLabel": "Tools",
				"ActionableIDs": ["one", "two"],
				"Actions": [{"Type": "url", "Name": "Look", "TemplateURL": "https://x/_VALUE_", "TriggerPlaceholder": "_VALUE_"}]
			}]
		}]
	}`)
	var resp ActionableTriggerListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Type != "actionable_trigger" || resp.TotalCount != 2 || resp.CursorNext != "abc" {
		t.Fatalf("list fields did not decode: %+v", resp.BaseListResponse)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("got %d groups, want 1", len(resp.Results))
	}
	g := resp.Results[0]
	if g.Pattern != "(a+)+" || g.ActivatesOn != "always" {
		t.Errorf("group did not decode: %+v", g)
	}
	if g.Complexity.Severity != ACTIONABLE_TRIGGER_SEVERITY_HIGH {
		t.Errorf("Severity = %q, want high", g.Complexity.Severity)
	}
	if len(g.Complexity.Reasons) != 1 {
		t.Errorf("got %d reasons, want 1", len(g.Complexity.Reasons))
	}
	if len(g.Menus) != 1 {
		t.Fatalf("got %d menus, want 1", len(g.Menus))
	}
	m := g.Menus[0]
	if m.MenuLabel != "Tools" || len(m.ActionableIDs) != 2 || len(m.Actions) != 1 {
		t.Errorf("menu did not decode: %+v", m)
	}
	if m.Actions[0].Type != ACTIONABLE_COMMAND_URL {
		t.Errorf("action Type = %q, want url", m.Actions[0].Type)
	}
}
