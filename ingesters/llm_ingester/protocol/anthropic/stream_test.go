/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package anthropic

import (
	"testing"

	"github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol"
)

func findStreamEvent(p *protocol.ParsedResponse, typ string) *protocol.Event {
	for i := range p.Events {
		if p.Events[i].Type == typ {
			return &p.Events[i]
		}
	}
	return nil
}

// A full Messages SSE stream: message_start, a text block, a tool_use block
// whose input arrives in JSON fragments, then message_delta + message_stop.
// Each event carries the redundant `event:` line the real API sends, to prove
// we dispatch on the JSON "type" and ignore it.
const sampleStream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_x","model":"claude-opus-4-8","usage":{"input_tokens":50,"cache_read_input_tokens":10,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_a","name":"get_weather"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"loc\""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":":\"sf\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":18}}

event: message_stop
data: {"type":"message_stop"}

`

func TestSSEReassemblerFull(t *testing.T) {
	r := newSSEReassembler()
	if err := r.Feed([]byte(sampleStream)); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	p, err := r.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if p.RequestID != "msg_x" || p.Model != "claude-opus-4-8" {
		t.Errorf("id=%q model=%q", p.RequestID, p.Model)
	}
	asst := findStreamEvent(p, protocol.EventAssistantMessage)
	if asst == nil || string(asst.Content) != "Hello" {
		t.Fatalf("assistant event = %v", asst)
	}
	tc := findStreamEvent(p, protocol.EventToolCall)
	if tc == nil || tc.ToolName != "get_weather" || tc.ToolCallID != "toolu_a" {
		t.Fatalf("tool_call event = %+v", tc)
	}
	if string(tc.Content) != `{"loc":"sf"}` {
		t.Errorf("tool_call input = %q", tc.Content)
	}
	// text (index 0) must precede the tool_use (index 1)
	var iText, iTool = -1, -1
	for i := range p.Events {
		switch p.Events[i].Type {
		case protocol.EventAssistantMessage:
			iText = i
		case protocol.EventToolCall:
			iTool = i
		}
	}
	if iText < 0 || iTool < 0 || iText > iTool {
		t.Errorf("text (%d) should precede tool_use (%d)", iText, iTool)
	}
	usage := findStreamEvent(p, protocol.EventUsage)
	if usage == nil || usage.Usage == nil {
		t.Fatal("no usage event")
	}
	// prompt = input(50) + cache_read(10) = 60; completion = 18; total = 78
	if usage.Usage.PromptTokens != 60 || usage.Usage.CompletionTokens != 18 || usage.Usage.TotalTokens != 78 {
		t.Errorf("usage = %+v", usage.Usage)
	}
}

func TestSSEReassemblerFragmentedFeed(t *testing.T) {
	// Feeding one byte at a time exercises fragment buffering across arbitrary
	// chunk boundaries.
	r := newSSEReassembler()
	for i := 0; i < len(sampleStream); i++ {
		if err := r.Feed([]byte{sampleStream[i]}); err != nil {
			t.Fatalf("Feed: %v", err)
		}
	}
	p, err := r.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	asst := findStreamEvent(p, protocol.EventAssistantMessage)
	if asst == nil || string(asst.Content) != "Hello" {
		t.Fatalf("assistant event = %v", asst)
	}
	tc := findStreamEvent(p, protocol.EventToolCall)
	if tc == nil || string(tc.Content) != `{"loc":"sf"}` {
		t.Fatalf("tool_call input = %q", tc.Content)
	}
}

func TestSSEReassemblerThinking(t *testing.T) {
	r := newSSEReassembler()
	stream := `event: message_start
data: {"type":"message_start","message":{"id":"t","model":"m","usage":{"input_tokens":5,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm "}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"ok"}}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"answer"}}

event: message_stop
data: {"type":"message_stop"}

`
	if err := r.Feed([]byte(stream)); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	p, err := r.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	re := findStreamEvent(p, protocol.EventReasoning)
	if re == nil || string(re.Content) != "hmm ok" {
		t.Fatalf("reasoning event = %v", re)
	}
	if p.Events[0].Type != protocol.EventReasoning {
		t.Errorf("first event = %q, want reasoning first", p.Events[0].Type)
	}
	asst := findStreamEvent(p, protocol.EventAssistantMessage)
	if asst == nil || string(asst.Content) != "answer" {
		t.Fatalf("assistant event = %v", asst)
	}
}

func TestSSEReassemblerEmptyStream(t *testing.T) {
	r := newSSEReassembler()
	// A stray non-event chunk with no message_start / blocks / usage is empty.
	if err := r.Feed([]byte("event: ping\ndata: {\"type\":\"ping\"}\n\n")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if _, err := r.Finalize(); err == nil {
		t.Error("expected error for empty stream, got nil")
	}
}

// The Messages API reports output_tokens as 1 on message_start regardless of
// what the response actually costs. That placeholder must not survive into the
// usage record: a stream that ends without a message_delta should report 0
// completion tokens rather than the API's made-up 1.
func TestSSEReassemblerMessageStartOutputTokensIgnored(t *testing.T) {
	const stream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_y","model":"claude-opus-4-8","usage":{"input_tokens":50,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_stop
data: {"type":"message_stop"}

`
	r := newSSEReassembler()
	if err := r.Feed([]byte(stream)); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	p, err := r.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	ev := findStreamEvent(p, protocol.EventUsage)
	if ev == nil || ev.Usage == nil {
		t.Fatal("no usage event")
	}
	if ev.Usage.CompletionTokens != 0 {
		t.Errorf("CompletionTokens = %d, want 0 (message_start's placeholder 1 must be ignored)",
			ev.Usage.CompletionTokens)
	}
	if ev.Usage.PromptTokens != 50 {
		t.Errorf("PromptTokens = %d, want 50", ev.Usage.PromptTokens)
	}
	if ev.Usage.TotalTokens != 50 {
		t.Errorf("TotalTokens = %d, want 50", ev.Usage.TotalTokens)
	}
}

// A real count from message_delta still wins.
func TestSSEReassemblerMessageDeltaOutputTokensUsed(t *testing.T) {
	r := newSSEReassembler()
	if err := r.Feed([]byte(sampleStream)); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	p, err := r.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	ev := findStreamEvent(p, protocol.EventUsage)
	if ev == nil || ev.Usage == nil {
		t.Fatal("no usage event")
	}
	if ev.Usage.CompletionTokens != 18 {
		t.Errorf("CompletionTokens = %d, want 18", ev.Usage.CompletionTokens)
	}
}
