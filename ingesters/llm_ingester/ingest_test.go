/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"sync"
	"testing"

	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/ingesters/llm_ingester/protocol"
)

// capture is a test entWriter that records every entry handed to it. It
// satisfies the (unexported) entWriter interface consumed by
// processors.NewProcessorSet because all of that interface's methods are
// exported.
type capture struct {
	mu   sync.Mutex
	ents []*entry.Entry
}

func (c *capture) WriteEntry(e *entry.Entry) error {
	c.mu.Lock()
	c.ents = append(c.ents, e)
	c.mu.Unlock()
	return nil
}

func (c *capture) WriteEntryContext(_ context.Context, e *entry.Entry) error {
	return c.WriteEntry(e)
}

func (c *capture) WriteBatch(ents []*entry.Entry) error {
	c.mu.Lock()
	c.ents = append(c.ents, ents...)
	c.mu.Unlock()
	return nil
}

func (c *capture) WriteBatchContext(_ context.Context, ents []*entry.Entry) error {
	return c.WriteBatch(ents)
}

// eventTypes extracts the event_type EV from every captured entry, in order.
func (c *capture) eventTypes(t *testing.T) []string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, e := range c.ents {
		v, ok := e.GetEnumeratedValue("event_type")
		if !ok {
			t.Fatalf("captured entry missing event_type EV")
		}
		s, ok := v.(string)
		if !ok {
			t.Fatalf("event_type EV is %T, want string", v)
		}
		out = append(out, s)
	}
	return out
}

// newCapturingCtx wires an emitCtx to a capturing processor set.
func newCapturingCtx(logMode string, toolCalls, usage bool) (*emitCtx, *capture) {
	c := &capture{}
	ec := &emitCtx{
		pproc:        processors.NewProcessorSet(c),
		listenerName: "test",
		protocolName: "openai-chat",
		logMode:      logMode,
		logToolCalls: toolCalls,
		logUsage:     usage,
	}
	return ec, c
}

func countType(types []string, want string) int {
	n := 0
	for _, tp := range types {
		if tp == want {
			n++
		}
	}
	return n
}

// sampleConversation returns a request-side event slice resembling a multi-turn
// conversation: system, user1, assistant1, user2.
func sampleConversation() []protocol.Event {
	return []protocol.Event{
		{Type: protocol.EventSystemMessage, Role: "system", Content: []byte("sys")},
		{Type: protocol.EventUserMessage, Role: "user", Content: []byte("u1")},
		{Type: protocol.EventAssistantMessage, Role: "assistant", Content: []byte("a1")},
		{Type: protocol.EventUserMessage, Role: "user", Content: []byte("u2")},
	}
}

func TestEmitRequestEventsUserOnly(t *testing.T) {
	ec, c := newCapturingCtx(logModeUserOnly, true, true)
	emitRequestEvents(ec, sampleConversation())
	types := c.eventTypes(t)
	if len(types) != 1 || types[0] != protocol.EventUserMessage {
		t.Fatalf("user-only should emit exactly the latest user message, got %v", types)
	}
	if string(c.ents[0].Data) != "u2" {
		t.Errorf("user-only emitted %q, want the latest user message u2", c.ents[0].Data)
	}
}

func TestEmitRequestEventsUserOnlyToolResults(t *testing.T) {
	// An agentic turn: a tool result from a prior turn, an assistant turn, then
	// the current turn's tool result.
	evs := []protocol.Event{
		{Type: protocol.EventUserMessage, Role: "user", Content: []byte("u1")},
		{Type: protocol.EventToolResult, Role: "tool", Content: []byte("old"), ToolCallID: "c0"},
		{Type: protocol.EventAssistantMessage, Role: "assistant", Content: []byte("a1")},
		{Type: protocol.EventToolResult, Role: "tool", Content: []byte("new"), ToolCallID: "c1"},
	}

	// tool logging on: latest user message + only the current turn's tool result
	ec, c := newCapturingCtx(logModeUserOnly, true, true)
	emitRequestEvents(ec, evs)
	types := c.eventTypes(t)
	if countType(types, protocol.EventUserMessage) != 1 {
		t.Errorf("user-only should emit the latest user message, got %v", types)
	}
	if got := countType(types, protocol.EventToolResult); got != 1 {
		t.Errorf("expected 1 tool_result from the current turn, got %d (%v)", got, types)
	}
	if countType(types, protocol.EventAssistantMessage) != 0 {
		t.Errorf("user-only must not emit assistant turns, got %v", types)
	}
	for _, e := range c.ents {
		if string(e.Data) == "old" {
			t.Errorf("user-only emitted a tool_result from a prior turn")
		}
	}

	// tool logging off: just the user message
	ec2, c2 := newCapturingCtx(logModeUserOnly, false, true)
	emitRequestEvents(ec2, evs)
	types2 := c2.eventTypes(t)
	if len(types2) != 1 || types2[0] != protocol.EventUserMessage {
		t.Errorf("user-only with tool logging off should emit just the user message, got %v", types2)
	}
}

func TestEmitRequestEventsFullConversation(t *testing.T) {
	ec, c := newCapturingCtx(logModeFullConversation, true, true)
	emitRequestEvents(ec, sampleConversation())
	types := c.eventTypes(t)
	// full-conversation emits every message in the body
	if len(types) != 4 {
		t.Fatalf("full-conversation should emit all 4 messages, got %v", types)
	}
}

func TestEmitRequestEventsDeltasNewSession(t *testing.T) {
	ec, c := newCapturingCtx(logModeDeltas, true, true)
	ec.newSession = true
	emitRequestEvents(ec, sampleConversation())
	types := c.eventTypes(t)
	// new session: system prompt + the tail (latest user turn after the last
	// assistant message => just u2).
	if countType(types, protocol.EventSystemMessage) != 1 {
		t.Errorf("new-session deltas should emit the system prompt once, got %v", types)
	}
	if countType(types, protocol.EventUserMessage) != 1 {
		t.Errorf("deltas should emit only the tail user message, got %v", types)
	}
	if countType(types, protocol.EventAssistantMessage) != 0 {
		t.Errorf("deltas should not re-emit prior assistant turns, got %v", types)
	}
}

func TestEmitRequestEventsDeltasContinuation(t *testing.T) {
	ec, c := newCapturingCtx(logModeDeltas, true, true)
	ec.newSession = false
	emitRequestEvents(ec, sampleConversation())
	types := c.eventTypes(t)
	// continuation: no system prompt, only the tail user turn.
	if countType(types, protocol.EventSystemMessage) != 0 {
		t.Errorf("continuation deltas should not re-emit the system prompt, got %v", types)
	}
	if len(types) != 1 || types[0] != protocol.EventUserMessage {
		t.Errorf("continuation deltas should emit just the tail user turn, got %v", types)
	}
}

func TestEmitRequestEventsDeltasToolResultTail(t *testing.T) {
	// A turn where the client sends back tool results after an assistant turn.
	evs := []protocol.Event{
		{Type: protocol.EventUserMessage, Role: "user", Content: []byte("u1")},
		{Type: protocol.EventAssistantMessage, Role: "assistant", Content: []byte("a1")},
		{Type: protocol.EventToolResult, Role: "tool", Content: []byte("result"), ToolCallID: "c1"},
	}
	// with tool logging on, the tool_result in the tail is emitted
	ec, c := newCapturingCtx(logModeDeltas, true, true)
	emitRequestEvents(ec, evs)
	if got := countType(c.eventTypes(t), protocol.EventToolResult); got != 1 {
		t.Errorf("expected 1 tool_result with logToolCalls=true, got %d", got)
	}
	// with tool logging off, it is suppressed
	ec2, c2 := newCapturingCtx(logModeDeltas, false, true)
	emitRequestEvents(ec2, evs)
	if got := countType(c2.eventTypes(t), protocol.EventToolResult); got != 0 {
		t.Errorf("expected 0 tool_result with logToolCalls=false, got %d", got)
	}
}

func TestEmitResponseEventsToggles(t *testing.T) {
	resp := []protocol.Event{
		{Type: protocol.EventAssistantMessage, Role: "assistant", Content: []byte("reply")},
		{Type: protocol.EventToolCall, Role: "assistant", ToolName: "f", ToolCallID: "c1"},
		{Type: protocol.EventUsage, Usage: &protocol.TokenUsage{TotalTokens: 5}},
	}

	// everything on
	ec, c := newCapturingCtx(logModeDeltas, true, true)
	emitResponseEvents(ec, resp)
	types := c.eventTypes(t)
	if countType(types, protocol.EventAssistantMessage) != 1 ||
		countType(types, protocol.EventToolCall) != 1 ||
		countType(types, protocol.EventUsage) != 1 {
		t.Fatalf("all-on should emit each response event once, got %v", types)
	}

	// tool calls + usage suppressed
	ec2, c2 := newCapturingCtx(logModeDeltas, false, false)
	emitResponseEvents(ec2, resp)
	types2 := c2.eventTypes(t)
	if countType(types2, protocol.EventToolCall) != 0 {
		t.Errorf("tool call should be suppressed, got %v", types2)
	}
	if countType(types2, protocol.EventUsage) != 0 {
		t.Errorf("usage should be suppressed, got %v", types2)
	}
	if countType(types2, protocol.EventAssistantMessage) != 1 {
		t.Errorf("assistant message should still be emitted, got %v", types2)
	}
}

// gravwell/issues#2679: Log-Mode=user was still ingesting assistant replies
// because emitResponseEvents ignored the log mode entirely.
func TestEmitResponseEventsUserMode(t *testing.T) {
	resp := []protocol.Event{
		{Type: protocol.EventAssistantMessage, Role: "assistant", Content: []byte("reply")},
		{Type: protocol.EventToolCall, Role: "assistant", ToolName: "f", ToolCallID: "c1"},
		{Type: protocol.EventUsage, Usage: &protocol.TokenUsage{TotalTokens: 5}},
	}

	// tool calls and usage still honor their own toggles
	ec, c := newCapturingCtx(logModeUserOnly, true, true)
	emitResponseEvents(ec, resp)
	types := c.eventTypes(t)
	if countType(types, protocol.EventAssistantMessage) != 0 {
		t.Errorf("user mode must not emit assistant messages, got %v", types)
	}
	if countType(types, protocol.EventToolCall) != 1 {
		t.Errorf("user mode should still emit tool calls when enabled, got %v", types)
	}
	if countType(types, protocol.EventUsage) != 1 {
		t.Errorf("user mode should still emit usage when enabled, got %v", types)
	}

	// with both toggles off, user mode emits nothing from the response
	ec2, c2 := newCapturingCtx(logModeUserOnly, false, false)
	emitResponseEvents(ec2, resp)
	if types2 := c2.eventTypes(t); len(types2) != 0 {
		t.Errorf("user mode with all toggles off should emit nothing, got %v", types2)
	}
}

func TestBuildEntryEnumeratedValues(t *testing.T) {
	ec, _ := newCapturingCtx(logModeDeltas, true, true)
	ec.sessionID = "sess-1"
	ec.requestID = "req-1"
	ec.model = "gpt-4o"
	ec.upstreamCode = 200
	ec.durationMs = 42
	ec.stream = true
	ec.newSession = true

	ev := protocol.Event{
		Type:       protocol.EventToolCall,
		Role:       "assistant",
		ToolName:   "get_weather",
		ToolCallID: "call_1",
		Content:    []byte("{}"),
		Usage:      &protocol.TokenUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}
	e := buildEntry(ec, ev)

	checks := map[string]any{
		"event_type":        protocol.EventToolCall,
		"role":              "assistant",
		"tool_name":         "get_weather",
		"tool_call_id":      "call_1",
		"prompt_tokens":     int64(1),
		"completion_tokens": int64(2),
		"total_tokens":      int64(3),
		"session_id":        "sess-1",
		"request_id":        "req-1",
		"model":             "gpt-4o",
		"protocol":          "openai-chat",
		"listener":          "test",
		"upstream_status":   int64(200),
		"duration_ms":       int64(42),
		"stream":            true,
		"new_session":       true,
	}
	for name, want := range checks {
		got, ok := e.GetEnumeratedValue(name)
		if !ok {
			t.Errorf("missing EV %q", name)
			continue
		}
		if got != want {
			t.Errorf("EV %q = %v (%T), want %v (%T)", name, got, got, want, want)
		}
	}
}
