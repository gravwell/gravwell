/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package protocol defines an LLM provider so the proxy core can parse
// requests/responses without knowing the vendor.
package protocol

import (
	"fmt"
	"sync"
)

// EventType values categorize a logged event.
const (
	EventUserMessage      = "request.user_message"
	EventSystemMessage    = "request.system_message"
	EventAssistantMessage = "response.assistant_message"
	EventToolCall         = "response.tool_call"
	EventToolResult       = "request.tool_result"
	EventUsage            = "response.usage"
)

// TokenUsage captures token accounting for a single response.
type TokenUsage struct {
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}

// Event is one logical thing worth logging.
// Content is the human-readable payload (e.g. the text of a message, the JSON
// of tool-call arguments). Structured fields go in their own columns so the
// ingest layer can promote them to enumerated values.
type Event struct {
	Type       string
	Role       string
	Content    []byte
	ToolName   string
	ToolCallID string
	Usage      *TokenUsage
}

// ParsedRequest is the result of parsing an inbound LLM request body.
type ParsedRequest struct {
	Model         string
	Stream        bool
	Events        []Event
	MessageHashes []string // canonical per-message hashes, ordered (for session prefix matching)
}

// ParsedResponse is the result of parsing an upstream LLM response.
type ParsedResponse struct {
	RequestID string
	Model     string
	Events    []Event
}

// StreamReassembler accumulates raw SSE bytes streamed from the upstream
// and produces a single ParsedResponse once the stream completes.
type StreamReassembler interface {
	// Feed receives a chunk of raw SSE bytes exactly as written by the
	// upstream. Implementations buffer until they have a full event.
	Feed(chunk []byte) error
	// Finalize is called when the upstream stream ends.
	Finalize() (*ParsedResponse, error)
}

// Protocol is implemented by each provider module.
type Protocol interface {
	// Name returns the registry key (e.g. "openai-chat").
	Name() string
	// Paths returns the URL paths this protocol expects to be exposed on.
	Paths() []string
	// ParseRequest parses an inbound (non-empty) request body.
	ParseRequest(body []byte, authHeader string) (*ParsedRequest, error)
	// ParseResponse parses a non-streaming response body.
	ParseResponse(body []byte) (*ParsedResponse, error)
	// NewStreamReassembler returns a fresh per-request streamer.
	NewStreamReassembler() StreamReassembler
}

var (
	mu       sync.RWMutex
	registry = map[string]Protocol{}
)

// Register adds a Protocol implementation to the global registry.
// Call from each provider package's init().
func Register(p Protocol) {
	if p == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	registry[p.Name()] = p
}

// Lookup returns the protocol registered under the given name.
func Lookup(name string) (Protocol, error) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown protocol %q", name)
	}
	return p, nil
}

// Names returns all registered protocol names, useful for diagnostics.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}
