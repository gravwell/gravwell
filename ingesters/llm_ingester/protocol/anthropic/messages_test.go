/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/ingesters/llm_ingester/protocol"
)

func findReqEvent(evs []protocol.Event, typ string) *protocol.Event {
	for i := range evs {
		if evs[i].Type == typ {
			return &evs[i]
		}
	}
	return nil
}

func TestParseRequestSystemUserToolResult(t *testing.T) {
	// A realistic agentic request: top-level system, a prior user turn, a prior
	// assistant tool_use turn, and a new user turn carrying a tool_result plus
	// human text.
	body := []byte(`{
		"model":"claude-opus-4-8",
		"system":"you are helpful",
		"messages":[
			{"role":"user","content":"what's the weather in sf?"},
			{"role":"assistant","content":[
				{"type":"text","text":"let me check"},
				{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"loc":"sf"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"toolu_1","content":"72F sunny"},
				{"type":"text","text":"thanks, and tomorrow?"}
			]}
		]
	}`)
	pr, err := messagesProtocol{}.ParseRequest(body, "")
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if pr.Model != "claude-opus-4-8" {
		t.Errorf("Model = %q", pr.Model)
	}
	if len(pr.MessageHashes) != 3 {
		t.Fatalf("MessageHashes len = %d, want 3", len(pr.MessageHashes))
	}
	sys := findReqEvent(pr.Events, protocol.EventSystemMessage)
	if sys == nil || string(sys.Content) != "you are helpful" {
		t.Fatalf("system event = %v", sys)
	}
	// The assistant turn is flattened to a single assistant event (its text only).
	asst := findReqEvent(pr.Events, protocol.EventAssistantMessage)
	if asst == nil || string(asst.Content) != "let me check" {
		t.Fatalf("assistant event = %v", asst)
	}
	tr := findReqEvent(pr.Events, protocol.EventToolResult)
	if tr == nil {
		t.Fatal("no tool_result event")
	}
	if tr.Role != roleTool || tr.ToolCallID != "toolu_1" || string(tr.Content) != "72F sunny" {
		t.Errorf("tool_result event = %+v", tr)
	}
	// Two user events: the initial question and the follow-up text.
	var userTexts []string
	for _, e := range pr.Events {
		if e.Type == protocol.EventUserMessage {
			userTexts = append(userTexts, string(e.Content))
		}
	}
	if len(userTexts) != 2 || userTexts[0] != "what's the weather in sf?" || userTexts[1] != "thanks, and tomorrow?" {
		t.Errorf("user events = %v", userTexts)
	}
}

func TestParseRequestNoMessages(t *testing.T) {
	if _, err := (messagesProtocol{}).ParseRequest([]byte(`{"model":"m","messages":[]}`), ""); err == nil {
		t.Error("expected error for request with no messages")
	}
}

func TestParseRequestSystemBlocks(t *testing.T) {
	// The system prompt may also be an array of text blocks.
	body := []byte(`{"model":"m","system":[{"type":"text","text":"a"},{"type":"text","text":"b"}],"messages":[{"role":"user","content":"hi"}]}`)
	pr, err := messagesProtocol{}.ParseRequest(body, "")
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	sys := findReqEvent(pr.Events, protocol.EventSystemMessage)
	if sys == nil || string(sys.Content) != "a\nb" {
		t.Fatalf("system event = %v", sys)
	}
}

func TestParseResponseTextThinkingToolAndUsage(t *testing.T) {
	body := []byte(`{
		"id":"msg_1",
		"model":"claude-opus-4-8",
		"role":"assistant",
		"content":[
			{"type":"thinking","thinking":"let me think"},
			{"type":"text","text":"here you go"},
			{"type":"tool_use","id":"toolu_9","name":"get_weather","input":{"loc":"sf"}}
		],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":100,"output_tokens":25,"cache_read_input_tokens":40,"cache_creation_input_tokens":10}
	}`)
	p, err := messagesProtocol{}.ParseResponse(body)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if p.RequestID != "msg_1" || p.Model != "claude-opus-4-8" {
		t.Errorf("id=%q model=%q", p.RequestID, p.Model)
	}
	re := findReqEvent(p.Events, protocol.EventReasoning)
	if re == nil || string(re.Content) != "let me think" {
		t.Fatalf("reasoning event = %v", re)
	}
	asst := findReqEvent(p.Events, protocol.EventAssistantMessage)
	if asst == nil || string(asst.Content) != "here you go" {
		t.Fatalf("assistant event = %v", asst)
	}
	// reasoning must precede the answer
	if p.Events[0].Type != protocol.EventReasoning {
		t.Errorf("first event = %q, want reasoning", p.Events[0].Type)
	}
	tc := findReqEvent(p.Events, protocol.EventToolCall)
	if tc == nil || tc.ToolName != "get_weather" || tc.ToolCallID != "toolu_9" {
		t.Fatalf("tool_call event = %+v", tc)
	}
	if string(tc.Content) != `{"loc":"sf"}` {
		t.Errorf("tool_call input = %q", tc.Content)
	}
	usage := findReqEvent(p.Events, protocol.EventUsage)
	if usage == nil || usage.Usage == nil {
		t.Fatal("no usage event")
	}
	// prompt = input(100) + cache_read(40) + cache_creation(10) = 150; total = 175
	if usage.Usage.PromptTokens != 150 || usage.Usage.CompletionTokens != 25 || usage.Usage.TotalTokens != 175 {
		t.Errorf("usage = %+v", usage.Usage)
	}
}

func TestParseResponseInvalid(t *testing.T) {
	if _, err := (messagesProtocol{}).ParseResponse([]byte("not json")); err == nil {
		t.Error("expected error for invalid response body")
	}
}

func TestParseResponseEmptyContentNoUsage(t *testing.T) {
	body := []byte(`{"id":"m","model":"x","role":"assistant","content":[]}`)
	p, err := messagesProtocol{}.ParseResponse(body)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if len(p.Events) != 0 {
		t.Errorf("expected no events, got %d: %+v", len(p.Events), p.Events)
	}
}

func TestFlattenText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"string", `"hello"`, "hello"},
		{"empty-string", `""`, ""},
		{"text-blocks", `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`, "a\nb"},
		{"tool-result-string", `"result body"`, "result body"},
		{"image-only", `[{"type":"image","source":{}}]`, `[{"type":"image","source":{}}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := flattenText(json.RawMessage(tt.raw))
			if string(got) != tt.want {
				t.Errorf("flattenText(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
	if got := flattenText(nil); got != nil {
		t.Errorf("flattenText(nil) = %q, want nil", got)
	}
}

func TestHashMessageStable(t *testing.T) {
	// Two requests that share a conversation prefix must hash the shared messages
	// identically so the session tracker recognizes the continuation.
	bodyA := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	bodyB := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"ok"},{"role":"user","content":"next"}]}`)
	a, err := messagesProtocol{}.ParseRequest(bodyA, "")
	if err != nil {
		t.Fatalf("ParseRequest A: %v", err)
	}
	b, err := messagesProtocol{}.ParseRequest(bodyB, "")
	if err != nil {
		t.Fatalf("ParseRequest B: %v", err)
	}
	if len(b.MessageHashes) < 1 {
		t.Fatalf("expected hashes in B")
	}
	if a.MessageHashes[0] != b.MessageHashes[0] {
		t.Fatal("message hash for identical first message diverged")
	}
}

// Newer models take operator instructions as role:"system" entries inside the
// messages array; Claude Code injects tool and subagent context that way, so
// those turns have to be logged like the top-level system prompt.
func TestParseRequestMidConversationSystem(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-5",
		"system":[{"type":"text","text":"top-level prompt"}],
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"system","content":"available tools: Bash, Read"},
			{"role":"assistant","content":"ok"},
			{"role":"system","content":[{"type":"text","text":"policy update"}]},
			{"role":"user","content":"go"}
		]
	}`)
	pr, err := messagesProtocol{}.ParseRequest(body, "")
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if len(pr.MessageHashes) != 5 {
		t.Fatalf("MessageHashes len = %d, want 5", len(pr.MessageHashes))
	}
	var sys []string
	for _, e := range pr.Events {
		if e.Type == protocol.EventSystemMessage {
			if e.Role != roleSystem {
				t.Errorf("system event role = %q, want %q", e.Role, roleSystem)
			}
			sys = append(sys, string(e.Content))
		}
	}
	want := []string{"top-level prompt", "available tools: Bash, Read", "policy update"}
	if len(sys) != len(want) {
		t.Fatalf("system events = %v, want %v", sys, want)
	}
	for i := range want {
		if sys[i] != want[i] {
			t.Errorf("system event %d = %q, want %q", i, sys[i], want[i])
		}
	}
}

// An empty system turn carries nothing to log but must still be hashed so
// session prefix matching lines up with the request the client sent.
func TestParseRequestEmptySystemTurn(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"system","content":""},{"role":"user","content":"hi"}]}`)
	pr, err := messagesProtocol{}.ParseRequest(body, "")
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if len(pr.MessageHashes) != 2 {
		t.Errorf("MessageHashes len = %d, want 2", len(pr.MessageHashes))
	}
	if e := findReqEvent(pr.Events, protocol.EventSystemMessage); e != nil {
		t.Errorf("empty system turn produced an event: %q", e.Content)
	}
}

// Content-block types this module does not model (images, documents, whatever
// the API adds next) must not vanish from the logged prompt. The OpenAI module
// falls back to the raw JSON for parts it cannot flatten; this does the same.
func TestParseRequestUnknownBlockNotDropped(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-8",
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"what is in this picture?"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}}
			]}
		]}`)
	req, err := messagesProtocol{}.ParseRequest(body, "")
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	ev := findReqEvent(req.Events, protocol.EventUserMessage)
	if ev == nil {
		t.Fatal("no user message event")
	}
	got := string(ev.Content)
	if !strings.Contains(got, "what is in this picture?") {
		t.Errorf("user text missing from %q", got)
	}
	if !strings.Contains(got, "iVBORw0KGgo=") {
		t.Errorf("image block was silently dropped, content = %q", got)
	}
}

// An unmodelled block carries no fields this module reads, so before the
// default branch in hashMessage two different images produced the same hash and
// the prefix matcher saw two distinct conversations as one.
func TestHashMessageDistinguishesUnknownBlocks(t *testing.T) {
	mk := func(data string) json.RawMessage {
		return json.RawMessage(`[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + data + `"}}]`)
	}
	a := hashMessage("user", mk("AAAA"))
	b := hashMessage("user", mk("BBBB"))
	if a == b {
		t.Errorf("two different image blocks hash alike (%s)", a)
	}
	// Still stable for the same input.
	if a != hashMessage("user", mk("AAAA")) {
		t.Error("hash is not stable for identical unknown blocks")
	}
	// And an unknown block still differs from a text block.
	if a == hashMessage("user", json.RawMessage(`[{"type":"text","text":"AAAA"}]`)) {
		t.Error("unknown block collides with a text block")
	}
}
