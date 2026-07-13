/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package openai

import (
	"encoding/json"
	"testing"

	"github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol"
)

func TestParseResponseTextToolAndUsage(t *testing.T) {
	body := []byte(`{
		"id":"resp-1",
		"model":"gpt-4o",
		"choices":[{"index":0,"message":{"role":"assistant","content":"hi there",
			"tool_calls":[{"id":"call_1","type":"function",
				"function":{"name":"get_weather","arguments":"{\"loc\":\"sf\"}"}}]},
			"finish_reason":"tool_calls"}],
		"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}
	}`)
	p, err := chatProtocol{}.ParseResponse(body)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if p.RequestID != "resp-1" {
		t.Errorf("RequestID = %q, want resp-1", p.RequestID)
	}
	if p.Model != "gpt-4o" {
		t.Errorf("Model = %q, want gpt-4o", p.Model)
	}
	asst := findEvent(p, protocol.EventAssistantMessage)
	if asst == nil || string(asst.Content) != "hi there" {
		t.Fatalf("assistant message = %v", asst)
	}
	tc := findEvent(p, protocol.EventToolCall)
	if tc == nil {
		t.Fatal("no tool_call event")
	}
	if tc.ToolName != "get_weather" || tc.ToolCallID != "call_1" {
		t.Errorf("tool_call = name %q id %q", tc.ToolName, tc.ToolCallID)
	}
	if string(tc.Content) != `{"loc":"sf"}` {
		t.Errorf("tool_call args = %q", tc.Content)
	}
	usage := findEvent(p, protocol.EventUsage)
	if usage == nil || usage.Usage == nil {
		t.Fatal("no usage event")
	}
	if usage.Usage.TotalTokens != 18 || usage.Usage.PromptTokens != 11 || usage.Usage.CompletionTokens != 7 {
		t.Errorf("usage = %+v", usage.Usage)
	}
}

func TestParseResponseNoUsageNoContent(t *testing.T) {
	// A response with an empty assistant message and no usage should not
	// synthesize spurious events.
	body := []byte(`{"id":"r","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":""}}]}`)
	p, err := chatProtocol{}.ParseResponse(body)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if len(p.Events) != 0 {
		t.Errorf("expected no events, got %d: %+v", len(p.Events), p.Events)
	}
}

func TestParseResponseInvalid(t *testing.T) {
	if _, err := (chatProtocol{}).ParseResponse([]byte("not json")); err == nil {
		t.Error("expected error for invalid response body")
	}
}

func TestParseResponseReasoning(t *testing.T) {
	// Reasoning arrives under different field names depending on the provider;
	// both should surface as a reasoning event ordered ahead of the answer.
	for _, field := range []string{"reasoning", "reasoning_content"} {
		t.Run(field, func(t *testing.T) {
			body := []byte(`{"id":"r","model":"m","choices":[{"index":0,"message":{"role":"assistant","` +
				field + `":"let me think","content":"the answer"}}]}`)
			p, err := chatProtocol{}.ParseResponse(body)
			if err != nil {
				t.Fatalf("ParseResponse: %v", err)
			}
			re := findEvent(p, protocol.EventReasoning)
			if re == nil || string(re.Content) != "let me think" {
				t.Fatalf("reasoning event = %v", re)
			}
			if re.Role != roleAssistant {
				t.Errorf("reasoning role = %q, want %q", re.Role, roleAssistant)
			}
			// reasoning must be emitted before the assistant answer
			var iReason, iAnswer = -1, -1
			for i := range p.Events {
				switch p.Events[i].Type {
				case protocol.EventReasoning:
					iReason = i
				case protocol.EventAssistantMessage:
					iAnswer = i
				}
			}
			if iReason < 0 || iAnswer < 0 || iReason > iAnswer {
				t.Errorf("reasoning (%d) should precede answer (%d)", iReason, iAnswer)
			}
		})
	}
}

func TestParseRequestNoMessages(t *testing.T) {
	if _, err := (chatProtocol{}).ParseRequest([]byte(`{"model":"m","messages":[]}`), ""); err == nil {
		t.Error("expected error for request with no messages")
	}
}

func TestContentToBytes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"string", `"hello"`, "hello"},
		{"empty-string", `""`, ""},
		{"null", `null`, ""},
		{"text-part", `[{"type":"text","text":"hi"}]`, "hi"},
		{"multiple-text-parts", `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`, "a\nb"},
		{"input-text-variant", `[{"type":"input_text","text":"hi"}]`, "hi"},
		// text mixed with a non-text part: keep the text, drop the image.
		{"mixed-parts", `[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"x"}}]`, "look"},
		// no text parts at all: fall back to the raw JSON so nothing is lost.
		{"image-only", `[{"type":"image_url","image_url":{"url":"x"}}]`, `[{"type":"image_url","image_url":{"url":"x"}}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw json.RawMessage
			if tt.raw != "null" {
				raw = json.RawMessage(tt.raw)
			} else {
				raw = json.RawMessage("null")
			}
			got := contentToBytes(raw)
			if string(got) != tt.want {
				t.Errorf("contentToBytes(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
	// absent (nil) content
	if got := contentToBytes(nil); got != nil {
		t.Errorf("contentToBytes(nil) = %q, want nil", got)
	}
}

func TestMessageToEventRoles(t *testing.T) {
	tests := []struct {
		role     string
		wantType string
	}{
		{roleUser, protocol.EventUserMessage},
		{roleSystem, protocol.EventSystemMessage},
		{roleTool, protocol.EventToolResult},
		{roleAssistant, protocol.EventAssistantMessage},
		{"unknown", ""},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			ev := messageToEvent(chatMessage{Role: tt.role, Content: json.RawMessage(`"x"`)})
			if ev.Type != tt.wantType {
				t.Errorf("role %q -> type %q, want %q", tt.role, ev.Type, tt.wantType)
			}
		})
	}
	// tool role carries tool_call_id through
	ev := messageToEvent(chatMessage{Role: roleTool, ToolCallID: "call_9", Content: json.RawMessage(`"res"`)})
	if ev.ToolCallID != "call_9" {
		t.Errorf("tool_call_id = %q, want call_9", ev.ToolCallID)
	}
}
