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

func TestActionableTriggerUnmarshalString(t *testing.T) {
	data := []byte(`"^\\d+\\.\\d+\\.\\d+\\.\\d+$"`)
	var trigger ActionableTrigger
	if err := json.Unmarshal(data, &trigger); err != nil {
		t.Fatal(err)
	}
	if trigger.Pattern != `^\d+\.\d+\.\d+\.\d+$` {
		t.Fatalf("unexpected pattern: %q", trigger.Pattern)
	}
	if !trigger.Hyperlink {
		t.Fatal("string trigger should default to Hyperlink=true")
	}
	if trigger.Disabled {
		t.Fatal("string trigger should default to Disabled=false")
	}
}

func TestActionableTriggerUnmarshalObject(t *testing.T) {
	data := []byte(`{"pattern":"test.*","hyperlink":false,"disabled":true}`)
	var trigger ActionableTrigger
	if err := json.Unmarshal(data, &trigger); err != nil {
		t.Fatal(err)
	}
	if trigger.Pattern != "test.*" {
		t.Fatalf("unexpected pattern: %q", trigger.Pattern)
	}
	if trigger.Hyperlink {
		t.Fatal("expected Hyperlink=false")
	}
	if !trigger.Disabled {
		t.Fatal("expected Disabled=true")
	}
}

func TestActionableTriggerMarshal(t *testing.T) {
	trigger := ActionableTrigger{Pattern: "foo", Hyperlink: true, Disabled: false}
	data, err := json.Marshal(trigger)
	if err != nil {
		t.Fatal(err)
	}
	// Always marshals as object form.
	expected := `{"pattern":"foo","hyperlink":true,"disabled":false}`
	if string(data) != expected {
		t.Fatalf("expected %s, got %s", expected, string(data))
	}
}

func TestActionableContentRoundTrip(t *testing.T) {
	input := ActionableContent{
		MenuLabel: "My Pivot",
		Triggers: []ActionableTrigger{
			{Pattern: `\d+\.\d+\.\d+\.\d+`, Hyperlink: true, Disabled: false},
		},
		Actions: []ActionableAction{
			{
				Name:        "Query IP",
				Description: "Look up IP",
				Placeholder: "Enter IP",
				Start: &ActionableTimeVariable{
					Type:   "string",
					Format: "YYYY-MM-DD",
				},
				End: &ActionableTimeVariable{
					Type: "timestamp",
				},
				Command: ActionableCommand{
					Type:      ACTIONABLE_COMMAND_QUERY,
					Reference: "tag=netflow src==_VALUE_",
				},
			},
			{
				Name: "Open Dashboard",
				Command: ActionableCommand{
					Type:      ACTIONABLE_COMMAND_DASHBOARD,
					Reference: "some-uuid",
					Options:   &ActionableCommandOptions{Variable: "ip"},
				},
			},
			{
				Name:             "Open URL",
				NoValueURLEncode: true,
				Command: ActionableCommand{
					Type:      ACTIONABLE_COMMAND_URL,
					Reference: "https://example.com/lookup?ip=_VALUE_",
					Options: &ActionableCommandOptions{
						Modal:            true,
						ModalWidth:       "80",
						NoValueURLEncode: true,
					},
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

func TestActionableContentUnmarshalMixedTriggers(t *testing.T) {
	// Simulates JSON from the server that contains both string and object triggers.
	raw := `{
		"menuLabel": null,
		"triggers": [
			"plain-pattern",
			{"pattern":"obj-pattern","hyperlink":false,"disabled":true}
		],
		"actions": []
	}`
	var content ActionableContent
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if content.MenuLabel != "" {
		t.Fatalf("expected empty MenuLabel, got %q", content.MenuLabel)
	}
	if len(content.Triggers) != 2 {
		t.Fatalf("expected 2 triggers, got %d", len(content.Triggers))
	}

	// String trigger: defaults to Hyperlink=true, Disabled=false.
	tr0 := content.Triggers[0]
	if tr0.Pattern != "plain-pattern" || !tr0.Hyperlink || tr0.Disabled {
		t.Fatalf("unexpected string trigger: %+v", tr0)
	}

	// Object trigger: explicit values.
	tr1 := content.Triggers[1]
	if tr1.Pattern != "obj-pattern" || tr1.Hyperlink || !tr1.Disabled {
		t.Fatalf("unexpected object trigger: %+v", tr1)
	}
}
