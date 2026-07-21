/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package anthropic implements the protocol contract for Anthropic's
// /v1/messages endpoint (the Claude Messages API), including SSE streaming.
//
// The wire structs below model the subset of the Messages API we consume. They
// are derived from the official Anthropic API reference:
// https://docs.claude.com/en/api/messages
//
// The Messages API differs from OpenAI Chat Completions in ways this plugin
// normalizes into the shared protocol.Event model:
//   - the system prompt is a top-level field, not a role:"system" message;
//   - message content is a string OR an array of typed content blocks;
//   - tool *calls* are assistant-turn "tool_use" blocks and tool *results* are
//     user-turn "tool_result" blocks — there is no separate "tool" role;
//   - token usage has no total_tokens and reports cache tokens separately.
package anthropic

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
	protocolName = "anthropic-messages"
	messagesPath = "/v1/messages"

	roleSystem    = "system"
	roleUser      = "user"
	roleAssistant = "assistant"
	roleTool      = "tool"

	blockText       = "text"
	blockThinking   = "thinking"
	blockToolUse    = "tool_use"
	blockToolResult = "tool_result"
)

// messagesProtocol implements protocol.Protocol for Anthropic's Messages API.
type messagesProtocol struct{}

func init() {
	protocol.Register(messagesProtocol{})
}

func (messagesProtocol) Name() string    { return protocolName }
func (messagesProtocol) Paths() []string { return []string{messagesPath} }

// The following types mirror the Anthropic Messages request/response schema; see
// https://docs.claude.com/en/api/messages for the authoritative definitions.

// reqMessage is one entry of the request "messages" array. Content may be a
// JSON string OR an array of content blocks.
type reqMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// messagesRequest covers the subset of the request body we read. "system" is a
// top-level field (string or array of text blocks), distinct from "messages".
type messagesRequest struct {
	Model    string          `json:"model"`
	System   json.RawMessage `json:"system,omitempty"`
	Messages []reqMessage    `json:"messages"`
	Stream   bool            `json:"stream,omitempty"`
}

// contentBlock is one element of a content-block array on either side. Only the
// fields we consume are modeled; the union is flat so a single struct decodes
// every block type we care about.
type contentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`     // text
	Thinking string `json:"thinking,omitempty"` // thinking
	// tool_use (assistant turns)
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result (user turns). Content is a string OR an array of blocks.
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

// respUsage is the token-accounting object returned on responses (and on the
// message_start / message_delta streaming events). Anthropic reports cache
// tokens separately and does not report a total.
type respUsage struct {
	InputTokens        int64 `json:"input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	CacheCreationInput int64 `json:"cache_creation_input_tokens"`
	CacheReadInput     int64 `json:"cache_read_input_tokens"`
}

type messagesResponse struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Content []contentBlock `json:"content"`
	Usage   *respUsage     `json:"usage,omitempty"`
}

// ParseRequest extracts events and a per-message session fingerprint from a
// Messages API request body.
func (messagesProtocol) ParseRequest(body []byte, authHeader string) (*protocol.ParsedRequest, error) {
	var req messagesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("invalid messages request body: %w", err)
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("messages request has no messages")
	}
	pr := &protocol.ParsedRequest{
		Model:  req.Model,
		Stream: req.Stream,
	}
	// The system prompt is top-level in the Messages API. Emit it as a system
	// event so the delta logger can treat it like OpenAI's system message.
	if sys := flattenText(req.System); len(sys) > 0 {
		pr.Events = append(pr.Events, protocol.Event{
			Type:    protocol.EventSystemMessage,
			Role:    roleSystem,
			Content: sys,
		})
	}
	pr.MessageHashes = make([]string, len(req.Messages))
	for i, m := range req.Messages {
		pr.MessageHashes[i] = hashMessage(m.Role, m.Content)
		pr.Events = append(pr.Events, messageToEvents(m.Role, m.Content)...)
	}
	return pr, nil
}

// ParseResponse builds events from a non-streaming Messages response.
func (messagesProtocol) ParseResponse(body []byte) (*protocol.ParsedResponse, error) {
	var resp messagesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("invalid messages response body: %w", err)
	}
	out := &protocol.ParsedResponse{
		RequestID: resp.ID,
		Model:     resp.Model,
	}
	// Content blocks arrive in generation order (thinking before text/tool_use),
	// so preserving order gives us reasoning-before-answer for free.
	for _, b := range resp.Content {
		switch b.Type {
		case blockThinking:
			if b.Thinking != "" {
				out.Events = append(out.Events, protocol.Event{
					Type:    protocol.EventReasoning,
					Role:    roleAssistant,
					Content: []byte(b.Thinking),
				})
			}
		case blockText:
			if b.Text != "" {
				out.Events = append(out.Events, protocol.Event{
					Type:    protocol.EventAssistantMessage,
					Role:    roleAssistant,
					Content: []byte(b.Text),
				})
			}
		case blockToolUse:
			out.Events = append(out.Events, protocol.Event{
				Type:       protocol.EventToolCall,
				Role:       roleAssistant,
				ToolName:   b.Name,
				ToolCallID: b.ID,
				Content:    []byte(b.Input),
			})
		}
	}
	if resp.Usage != nil {
		out.Events = append(out.Events, protocol.Event{
			Type:  protocol.EventUsage,
			Usage: usageToToken(resp.Usage),
		})
	}
	return out, nil
}

// NewStreamReassembler returns a fresh streamer for one SSE response.
func (messagesProtocol) NewStreamReassembler() protocol.StreamReassembler {
	return newSSEReassembler()
}

// messageToEvents turns a single request-side message into logged events. A user
// turn may contain both tool results and text; an assistant turn is flattened to
// a single assistant event (it is a prior turn already logged from a response,
// and the flattening keeps the delta logger's boundary detection intact).
func messageToEvents(role string, raw json.RawMessage) []protocol.Event {
	str, blocks, isArray := decodeContent(raw)
	switch role {
	case roleAssistant:
		var content []byte
		if isArray {
			content = joinTextBlocks(blocks)
		} else {
			content = []byte(str)
		}
		return []protocol.Event{{
			Type:    protocol.EventAssistantMessage,
			Role:    roleAssistant,
			Content: content,
		}}
	case roleUser:
		if !isArray {
			return []protocol.Event{{
				Type:    protocol.EventUserMessage,
				Role:    roleUser,
				Content: []byte(str),
			}}
		}
		var evs []protocol.Event
		var text strings.Builder
		for _, b := range blocks {
			switch b.Type {
			case blockToolResult:
				evs = append(evs, protocol.Event{
					Type:       protocol.EventToolResult,
					Role:       roleTool,
					ToolCallID: b.ToolUseID,
					Content:    flattenText(b.Content),
				})
			case blockText:
				if b.Text == "" {
					continue
				}
				if text.Len() > 0 {
					text.WriteByte('\n')
				}
				text.WriteString(b.Text)
			}
		}
		if text.Len() > 0 {
			evs = append(evs, protocol.Event{
				Type:    protocol.EventUserMessage,
				Role:    roleUser,
				Content: []byte(text.String()),
			})
		}
		return evs
	}
	return nil
}

// decodeContent interprets a content field that may be a JSON string or an array
// of content blocks. isArray reports which form was found; unparseable input is
// returned as a raw string so nothing is silently dropped.
func decodeContent(raw json.RawMessage) (str string, blocks []contentBlock, isArray bool) {
	if len(raw) == 0 {
		return "", nil, false
	}
	if err := json.Unmarshal(raw, &str); err == nil {
		return str, nil, false
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return "", blocks, true
	}
	return string(raw), nil, false
}

// joinTextBlocks concatenates the text of the text blocks (newline-separated).
func joinTextBlocks(blocks []contentBlock) []byte {
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type != blockText || blk.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(blk.Text)
	}
	if b.Len() == 0 {
		return nil
	}
	return []byte(b.String())
}

// flattenText renders a content field (string or array of blocks) to its
// human-readable text. For the array form it pulls the text blocks; when an
// array carries no text (e.g. an image-only or block-structured tool result) it
// falls back to the raw JSON so nothing is silently dropped.
func flattenText(raw json.RawMessage) []byte {
	str, blocks, isArray := decodeContent(raw)
	if !isArray {
		if str == "" {
			return nil
		}
		return []byte(str)
	}
	if b := joinTextBlocks(blocks); b != nil {
		return b
	}
	return []byte(raw)
}

// hashMessage produces a stable per-message hash for session-prefix matching.
// It hashes role + canonical content so re-sending the same prior turns matches.
func hashMessage(role string, raw json.RawMessage) string {
	h := sha256.New()
	h.Write([]byte(role))
	h.Write([]byte{0})
	str, blocks, isArray := decodeContent(raw)
	if !isArray {
		h.Write([]byte(str))
		return hex.EncodeToString(h.Sum(nil))
	}
	for _, b := range blocks {
		h.Write([]byte{0})
		h.Write([]byte(b.Type))
		switch b.Type {
		case blockText:
			h.Write([]byte{0})
			h.Write([]byte(b.Text))
		case blockThinking:
			h.Write([]byte{0})
			h.Write([]byte(b.Thinking))
		case blockToolUse:
			h.Write([]byte{0})
			h.Write([]byte(b.ID))
			h.Write([]byte{0})
			h.Write([]byte(b.Name))
			h.Write([]byte{0})
			h.Write(b.Input)
		case blockToolResult:
			h.Write([]byte{0})
			h.Write([]byte(b.ToolUseID))
			h.Write([]byte{0})
			h.Write(flattenText(b.Content))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// usageToToken folds Anthropic's separate cache token counts into the shared
// TokenUsage model: prompt = input + cache-read + cache-creation, and total is
// synthesized (the Messages API does not report a total).
func usageToToken(u *respUsage) *protocol.TokenUsage {
	prompt := u.InputTokens + u.CacheReadInput + u.CacheCreationInput
	return &protocol.TokenUsage{
		PromptTokens:     prompt,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      prompt + u.OutputTokens,
	}
}
