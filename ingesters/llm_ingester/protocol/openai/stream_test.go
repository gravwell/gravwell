/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package openai

import (
	"testing"

	"github.com/gravwell/gravwell/v4/ingesters/llm_ingester/protocol"
)

func feedAll(t *testing.T, r protocol.StreamReassembler, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if err := r.Feed([]byte(p)); err != nil {
			t.Fatalf("Feed: %v", err)
		}
	}
}

func findEvent(p *protocol.ParsedResponse, typ string) *protocol.Event {
	for i := range p.Events {
		if p.Events[i].Type == typ {
			return &p.Events[i]
		}
	}
	return nil
}

func TestSSEReassemblerTextOnly(t *testing.T) {
	r := newSSEReassembler()
	stream := `data: {"id":"x1","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}

data: {"id":"x1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"lo "}}]}

data: {"id":"x1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"world"}}]}

data: {"id":"x1","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	feedAll(t, r, stream)
	p, err := r.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if p.RequestID != "x1" {
		t.Errorf("RequestID = %q, want x1", p.RequestID)
	}
	if p.Model != "gpt-4o" {
		t.Errorf("Model = %q, want gpt-4o", p.Model)
	}
	ev := findEvent(p, protocol.EventAssistantMessage)
	if ev == nil {
		t.Fatal("no assistant message event")
	}
	if string(ev.Content) != "Hello world" {
		t.Errorf("content = %q, want %q", ev.Content, "Hello world")
	}
}

func TestSSEReassemblerFragmentedFeed(t *testing.T) {
	r := newSSEReassembler()
	full := `data: {"id":"y","model":"m","choices":[{"index":0,"delta":{"content":"abc"}}]}

data: {"id":"y","choices":[{"index":0,"delta":{"content":"def"}}]}

data: [DONE]

`
	// Feed one byte at a time to exercise fragment buffering.
	for i := 0; i < len(full); i++ {
		if err := r.Feed([]byte{full[i]}); err != nil {
			t.Fatalf("Feed: %v", err)
		}
	}
	p, err := r.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	ev := findEvent(p, protocol.EventAssistantMessage)
	if ev == nil || string(ev.Content) != "abcdef" {
		t.Fatalf("content = %q", ev.Content)
	}
}

func TestSSEReassemblerToolCalls(t *testing.T) {
	r := newSSEReassembler()
	stream := `data: {"id":"z","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"get_weather","arguments":"{\"loc\""}}]}}]}

data: {"id":"z","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"sf\"}"}}]}}]}

data: [DONE]

`
	feedAll(t, r, stream)
	p, err := r.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	ev := findEvent(p, protocol.EventToolCall)
	if ev == nil {
		t.Fatal("no tool_call event")
	}
	if ev.ToolName != "get_weather" {
		t.Errorf("tool_name = %q", ev.ToolName)
	}
	if ev.ToolCallID != "call_a" {
		t.Errorf("tool_call_id = %q", ev.ToolCallID)
	}
	if string(ev.Content) != `{"loc":"sf"}` {
		t.Errorf("args = %q", ev.Content)
	}
}

func TestSSEReassemblerUsage(t *testing.T) {
	r := newSSEReassembler()
	stream := `data: {"id":"u","model":"m","choices":[{"index":0,"delta":{"content":"hi"}}]}

data: {"id":"u","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}

data: [DONE]

`
	feedAll(t, r, stream)
	p, err := r.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	ev := findEvent(p, protocol.EventUsage)
	if ev == nil || ev.Usage == nil {
		t.Fatal("no usage event")
	}
	if ev.Usage.TotalTokens != 13 {
		t.Errorf("total_tokens = %d, want 13", ev.Usage.TotalTokens)
	}
}

func TestSSEReassemblerReasoning(t *testing.T) {
	// Interleaved reasoning + content fragments should accumulate into separate
	// events, with reasoning ordered ahead of the assistant answer.
	r := newSSEReassembler()
	stream := `data: {"id":"z","model":"m","choices":[{"index":0,"delta":{"role":"assistant","reasoning":"think"}}]}

data: {"id":"z","choices":[{"index":0,"delta":{"reasoning_content":"ing "}}]}

data: {"id":"z","choices":[{"index":0,"delta":{"content":"ans"}}]}

data: {"id":"z","choices":[{"index":0,"delta":{"content":"wer"}}]}

data: [DONE]

`
	feedAll(t, r, stream)
	p, err := r.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	re := findEvent(p, protocol.EventReasoning)
	if re == nil || string(re.Content) != "thinking " {
		t.Fatalf("reasoning event = %v", re)
	}
	asst := findEvent(p, protocol.EventAssistantMessage)
	if asst == nil || string(asst.Content) != "answer" {
		t.Fatalf("assistant event = %v", asst)
	}
	if p.Events[0].Type != protocol.EventReasoning {
		t.Errorf("first event = %q, want reasoning first", p.Events[0].Type)
	}
}

func TestSSEReassemblerReasoningOnly(t *testing.T) {
	// A stream that carries only reasoning (no content/tools/usage) is still a
	// non-empty response.
	r := newSSEReassembler()
	feedAll(t, r, `data: {"id":"ro","model":"m","choices":[{"index":0,"delta":{"reasoning":"hmm"}}]}

data: [DONE]

`)
	p, err := r.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if re := findEvent(p, protocol.EventReasoning); re == nil || string(re.Content) != "hmm" {
		t.Fatalf("reasoning event = %v", re)
	}
}

func TestSSEReassemblerEmptyStream(t *testing.T) {
	r := newSSEReassembler()
	feedAll(t, r, "data: [DONE]\n\n")
	if _, err := r.Finalize(); err == nil {
		t.Error("expected error for empty stream, got nil")
	}
}

func TestParseRequestHashesStable(t *testing.T) {
	p := chatProtocol{}
	bodyA := []byte(`{"model":"m","messages":[{"role":"system","content":"S"},{"role":"user","content":"hi"}]}`)
	bodyB := []byte(`{"model":"m","messages":[{"role":"system","content":"S"},{"role":"user","content":"hi"},{"role":"assistant","content":"ok"},{"role":"user","content":"next"}]}`)
	a, err := p.ParseRequest(bodyA, "")
	if err != nil {
		t.Fatalf("ParseRequest A: %v", err)
	}
	b, err := p.ParseRequest(bodyB, "")
	if err != nil {
		t.Fatalf("ParseRequest B: %v", err)
	}
	// The first two hashes must match: same canonical content.
	if len(b.MessageHashes) < 2 {
		t.Fatalf("expected at least 2 hashes in B, got %d", len(b.MessageHashes))
	}
	if a.MessageHashes[0] != b.MessageHashes[0] || a.MessageHashes[1] != b.MessageHashes[1] {
		t.Fatal("message hashes for identical messages diverged")
	}
}
