/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package openai implements the protocol contract for OpenAI's
// /v1/chat/completions endpoint, including SSE streaming.
package openai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol"
)

const (
	protocolName  = "openai-chat"
	chatPath      = "/v1/chat/completions"
	roleSystem    = "system"
	roleUser      = "user"
	roleAssistant = "assistant"
	roleTool      = "tool"
)

// chatProtocol implements protocol.Protocol for OpenAI's Chat Completions API.
type chatProtocol struct{}

func init() {
	protocol.Register(chatProtocol{})
}

func (chatProtocol) Name() string    { return protocolName }
func (chatProtocol) Paths() []string { return []string{chatPath} }

// chatMessage covers the subset of the message shape we care about.
// Content may be a string OR an array of content parts in the multimodal form.
type chatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []toolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

type chatUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
}

// ParseRequest extracts events and a session fingerprint from a chat-completions request body.
func (chatProtocol) ParseRequest(body []byte, authHeader string) (*protocol.ParsedRequest, error) {
	var req chatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("invalid chat request body: %w", err)
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("chat request has no messages")
	}
	pr := &protocol.ParsedRequest{
		Model:      req.Model,
		Stream:     req.Stream,
		APIKeyHash: hashBearer(authHeader),
	}
	pr.MessageHashes = make([]string, len(req.Messages))
	for i, m := range req.Messages {
		pr.MessageHashes[i] = hashMessage(m)
		ev := messageToEvent(m)
		if ev.Type != "" {
			pr.Events = append(pr.Events, ev)
		}
	}
	return pr, nil
}

// ParseResponse builds events from a non-streaming chat-completions response.
func (chatProtocol) ParseResponse(body []byte) (*protocol.ParsedResponse, error) {
	var resp chatResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("invalid chat response body: %w", err)
	}
	return buildParsedResponse(&resp), nil
}

// NewStreamReassembler returns a fresh streamer for one SSE response.
func (chatProtocol) NewStreamReassembler() protocol.StreamReassembler {
	return newSSEReassembler()
}

// messageToEvent turns a single request-side message into a logged event.
// For deltas mode, the proxy decides which events to actually emit; here we
// just convert each message to its event shape.
func messageToEvent(m chatMessage) protocol.Event {
	switch m.Role {
	case roleUser:
		return protocol.Event{
			Type:    protocol.EventUserMessage,
			Role:    roleUser,
			Content: contentToBytes(m.Content),
		}
	case roleSystem:
		return protocol.Event{
			Type:    protocol.EventSystemMessage,
			Role:    roleSystem,
			Content: contentToBytes(m.Content),
		}
	case roleTool:
		return protocol.Event{
			Type:       protocol.EventToolResult,
			Role:       roleTool,
			Content:    contentToBytes(m.Content),
			ToolCallID: m.ToolCallID,
		}
	case roleAssistant:
		// Assistant messages in the request are prior turns; we don't normally
		// re-log them per request — the proxy uses Log_Mode to decide.
		return protocol.Event{
			Type:    protocol.EventAssistantMessage,
			Role:    roleAssistant,
			Content: contentToBytes(m.Content),
		}
	}
	return protocol.Event{}
}

// buildParsedResponse converts an unmarshalled chatResponse to ParsedResponse.
func buildParsedResponse(resp *chatResponse) *protocol.ParsedResponse {
	out := &protocol.ParsedResponse{
		RequestID: resp.ID,
		Model:     resp.Model,
	}
	for _, ch := range resp.Choices {
		if text := contentToBytes(ch.Message.Content); len(text) > 0 {
			out.Events = append(out.Events, protocol.Event{
				Type:    protocol.EventAssistantMessage,
				Role:    roleAssistant,
				Content: text,
			})
		}
		for _, tc := range ch.Message.ToolCalls {
			out.Events = append(out.Events, protocol.Event{
				Type:       protocol.EventToolCall,
				Role:       roleAssistant,
				ToolName:   tc.Function.Name,
				ToolCallID: tc.ID,
				Content:    []byte(tc.Function.Arguments),
			})
		}
	}
	if resp.Usage != nil {
		out.Events = append(out.Events, protocol.Event{
			Type: protocol.EventUsage,
			Usage: &protocol.TokenUsage{
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				TotalTokens:      resp.Usage.TotalTokens,
			},
		})
	}
	return out
}

// contentToBytes flattens an OpenAI content field, which can be:
//   - a JSON string ("hello")
//   - an array of content parts ([{"type":"text","text":"hello"}, ...])
//   - null / absent
//
// We return UTF-8 text for the string case and the JSON itself for the array
// case so downstream still has structured access.
func contentToBytes(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	// try string first
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []byte(s)
	}
	// fall back to raw JSON (e.g. multimodal content parts)
	return []byte(raw)
}

// hashMessage produces a stable hash for one message used for session-prefix matching.
// We hash role + content so that re-sending the same prior turns matches.
func hashMessage(m chatMessage) string {
	h := sha256.New()
	h.Write([]byte(m.Role))
	h.Write([]byte{0})
	h.Write(contentToBytes(m.Content))
	// include tool-call structure for assistant tool turns
	if len(m.ToolCalls) > 0 {
		for _, tc := range m.ToolCalls {
			h.Write([]byte{0})
			h.Write([]byte(tc.ID))
			h.Write([]byte{0})
			h.Write([]byte(tc.Function.Name))
			h.Write([]byte{0})
			h.Write([]byte(tc.Function.Arguments))
		}
	}
	if m.ToolCallID != "" {
		h.Write([]byte{0})
		h.Write([]byte(m.ToolCallID))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// hashBearer turns an Authorization header value into a short opaque identifier
// suitable for grouping requests without storing the key itself. Returns empty
// string if no Bearer token is present.
func hashBearer(authHeader string) string {
	const prefix = "Bearer "
	authHeader = strings.TrimSpace(authHeader)
	if !strings.HasPrefix(authHeader, prefix) {
		return ""
	}
	token := strings.TrimSpace(authHeader[len(prefix):])
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8]) // 64-bit prefix is enough to group, not to recover the key
}
