/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package anthropic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol"
)

// sseReassembler accumulates Anthropic Messages streaming events into a single
// coherent ParsedResponse. It is fed raw bytes via Feed() and tolerates
// arbitrary chunk boundaries — it only acts on complete `\n\n`-terminated
// events.
//
// Unlike OpenAI's `data:{delta}` chunks, the Messages API is a named-event SSE
// protocol: message_start -> per-block content_block_start / content_block_delta
// / content_block_stop -> message_delta -> message_stop. The SSE framing is
// identical (events separated by "\n\n", payload on `data:` lines), so the Feed
// loop mirrors the OpenAI reassembler; dispatch happens on the JSON "type"
// field rather than the `event:` line.
type sseReassembler struct {
	// buf holds bytes not yet broken into events.
	buf bytes.Buffer
	// state being assembled from the event stream.
	id     string
	model  string
	blocks map[int]*streamBlock
	order  []int // content-block indices in first-seen order
	// token accounting: input/cache arrive on message_start, output on
	// message_delta.
	inputTokens   int64
	cacheRead     int64
	cacheCreation int64
	outputTokens  int64
	haveUsage     bool
	done          bool
	parseErr      error
}

// streamBlock accumulates one content block. typ is "text", "thinking", or
// "tool_use"; buf holds the streamed text, thinking, or tool-input JSON.
type streamBlock struct {
	typ  string
	id   string
	name string
	buf  bytes.Buffer
}

// streamEvent covers the subset of a Messages streaming event we read. The
// several event shapes share one struct keyed on Type; see
// https://docs.claude.com/en/api/messages-streaming for the authoritative
// schema.
type streamEvent struct {
	Type string `json:"type"`
	// message_start
	Message *struct {
		ID    string     `json:"id"`
		Model string     `json:"model"`
		Usage *respUsage `json:"usage,omitempty"`
	} `json:"message,omitempty"`
	// content_block_start / content_block_delta / content_block_stop
	Index        int `json:"index"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
	} `json:"content_block,omitempty"`
	Delta *struct {
		Type        string `json:"type,omitempty"`
		Text        string `json:"text,omitempty"`
		Thinking    string `json:"thinking,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
		StopReason  string `json:"stop_reason,omitempty"`
	} `json:"delta,omitempty"`
	// message_delta carries the running/final output token count.
	Usage *respUsage `json:"usage,omitempty"`
}

func newSSEReassembler() *sseReassembler {
	return &sseReassembler{blocks: map[int]*streamBlock{}}
}

// Feed accepts raw bytes from the upstream response body. It tolerates being
// called with byte fragments at arbitrary boundaries: only fully-terminated SSE
// events (ending in "\n\n") are processed; the remainder stays in the buffer.
func (r *sseReassembler) Feed(chunk []byte) error {
	if r.done {
		return nil
	}
	if _, err := r.buf.Write(chunk); err != nil {
		return err
	}
	for {
		idx := bytes.Index(r.buf.Bytes(), []byte("\n\n"))
		if idx < 0 {
			return nil
		}
		event := r.buf.Next(idx + 2)
		// strip trailing "\n\n"
		event = event[:len(event)-2]
		if err := r.processEvent(event); err != nil {
			r.parseErr = err
			// continue processing; we want the partial state we already built
		}
		if r.done {
			return nil
		}
	}
}

// processEvent parses one fully-terminated SSE event block. Each event may
// contain several `field: value\n` lines; we only look at `data:` lines (the
// `event:` line is redundant with the JSON "type" field).
func (r *sseReassembler) processEvent(event []byte) error {
	for _, line := range bytes.Split(event, []byte("\n")) {
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[len("data:"):])
		if len(payload) == 0 {
			continue
		}
		var e streamEvent
		if err := json.Unmarshal(payload, &e); err != nil {
			return fmt.Errorf("invalid SSE event: %w", err)
		}
		r.applyEvent(&e)
	}
	return nil
}

func (r *sseReassembler) applyEvent(e *streamEvent) {
	switch e.Type {
	case "message_start":
		if e.Message != nil {
			if r.id == "" {
				r.id = e.Message.ID
			}
			if r.model == "" {
				r.model = e.Message.Model
			}
			if u := e.Message.Usage; u != nil {
				r.inputTokens = u.InputTokens
				r.cacheRead = u.CacheReadInput
				r.cacheCreation = u.CacheCreationInput
				if u.OutputTokens > 0 {
					r.outputTokens = u.OutputTokens
				}
				r.haveUsage = true
			}
		}
	case "content_block_start":
		b := r.blockAt(e.Index)
		if e.ContentBlock != nil {
			b.typ = e.ContentBlock.Type
			b.id = e.ContentBlock.ID
			b.name = e.ContentBlock.Name
		}
	case "content_block_delta":
		if e.Delta == nil {
			return
		}
		b := r.blockAt(e.Index)
		switch e.Delta.Type {
		case "text_delta":
			if b.typ == "" {
				b.typ = blockText
			}
			b.buf.WriteString(e.Delta.Text)
		case "thinking_delta":
			if b.typ == "" {
				b.typ = blockThinking
			}
			b.buf.WriteString(e.Delta.Thinking)
		case "input_json_delta":
			if b.typ == "" {
				b.typ = blockToolUse
			}
			b.buf.WriteString(e.Delta.PartialJSON)
			// signature_delta and other delta types carry no logged content.
		}
	case "message_delta":
		if e.Usage != nil {
			if e.Usage.OutputTokens > 0 {
				r.outputTokens = e.Usage.OutputTokens
			}
			if e.Usage.InputTokens > 0 {
				r.inputTokens = e.Usage.InputTokens
			}
			r.haveUsage = true
		}
	case "message_stop":
		r.done = true
	case "error":
		r.parseErr = errors.New("upstream stream reported an error event")
	}
}

// blockAt returns the block for an index, creating it (and recording its order)
// on first sight so a content_block_delta that arrives without a preceding
// content_block_start is still captured.
func (r *sseReassembler) blockAt(idx int) *streamBlock {
	b, ok := r.blocks[idx]
	if !ok {
		b = &streamBlock{}
		r.blocks[idx] = b
		r.order = append(r.order, idx)
	}
	return b
}

// Finalize returns the assembled response. Safe to call multiple times.
func (r *sseReassembler) Finalize() (*protocol.ParsedResponse, error) {
	if r.parseErr != nil && r.id == "" && len(r.order) == 0 && !r.haveUsage {
		return nil, r.parseErr
	}
	if r.id == "" && len(r.order) == 0 && !r.haveUsage {
		return nil, errors.New("empty stream")
	}
	out := &protocol.ParsedResponse{
		RequestID: r.id,
		Model:     r.model,
	}
	// Blocks are keyed by index and Anthropic emits thinking before text/tool_use,
	// so ascending index order gives reasoning-before-answer.
	for _, idx := range r.order {
		b := r.blocks[idx]
		switch b.typ {
		case blockThinking:
			if b.buf.Len() > 0 {
				out.Events = append(out.Events, protocol.Event{
					Type:    protocol.EventReasoning,
					Role:    roleAssistant,
					Content: bytes.Clone(b.buf.Bytes()),
				})
			}
		case blockText:
			if b.buf.Len() > 0 {
				out.Events = append(out.Events, protocol.Event{
					Type:    protocol.EventAssistantMessage,
					Role:    roleAssistant,
					Content: bytes.Clone(b.buf.Bytes()),
				})
			}
		case blockToolUse:
			out.Events = append(out.Events, protocol.Event{
				Type:       protocol.EventToolCall,
				Role:       roleAssistant,
				ToolName:   b.name,
				ToolCallID: b.id,
				Content:    bytes.Clone(b.buf.Bytes()),
			})
		}
	}
	if r.haveUsage {
		out.Events = append(out.Events, protocol.Event{
			Type: protocol.EventUsage,
			Usage: usageToToken(&respUsage{
				InputTokens:        r.inputTokens,
				OutputTokens:       r.outputTokens,
				CacheReadInput:     r.cacheRead,
				CacheCreationInput: r.cacheCreation,
			}),
		})
	}
	return out, nil
}
