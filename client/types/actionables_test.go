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
