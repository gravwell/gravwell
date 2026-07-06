/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol"
)

// sseReassembler accumulates OpenAI Server-Sent-Events chunks into a
// single coherent ParsedResponse. It is fed raw bytes via Feed() and tolerates
// arbitrary chunk boundaries — it only acts on complete `\n\n`-terminated
// events.
type sseReassembler struct {
	// buf holds bytes not yet broken into events.
	buf bytes.Buffer
	// state being assembled from delta frames.
	id      string
	model   string
	content bytes.Buffer
	usage   *protocol.TokenUsage
	// per-index assistant tool calls (OpenAI streams arguments in fragments).
	tools    map[int]*partialTool
	toolKeys []int // preserve first-seen order
	done     bool
	parseErr error
}

type partialTool struct {
	id   string
	name string
	args bytes.Buffer
}

// streamDelta covers the subset of an OpenAI streaming chunk we read.
type streamDelta struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string `json:"role,omitempty"`
			Content   string `json:"content,omitempty"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id,omitempty"`
				Type     string `json:"type,omitempty"`
				Function struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				} `json:"function,omitempty"`
			} `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *chatUsage `json:"usage,omitempty"`
}

func newSSEReassembler() *sseReassembler {
	return &sseReassembler{tools: map[int]*partialTool{}}
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
// contain several `field: value\n` lines; we only look at `data:` lines.
func (r *sseReassembler) processEvent(event []byte) error {
	for _, line := range bytes.Split(event, []byte("\n")) {
		// Each SSE field line has the form `name: value` or `name:value`. We
		// only care about `data:`.
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[len("data:"):])
		if len(payload) == 0 {
			continue
		}
		if bytes.Equal(payload, []byte("[DONE]")) {
			r.done = true
			return nil
		}
		var d streamDelta
		if err := json.Unmarshal(payload, &d); err != nil {
			return fmt.Errorf("invalid SSE chunk: %w", err)
		}
		r.applyDelta(&d)
	}
	return nil
}

func (r *sseReassembler) applyDelta(d *streamDelta) {
	if d.ID != "" && r.id == "" {
		r.id = d.ID
	}
	if d.Model != "" && r.model == "" {
		r.model = d.Model
	}
	if d.Usage != nil {
		r.usage = &protocol.TokenUsage{
			PromptTokens:     d.Usage.PromptTokens,
			CompletionTokens: d.Usage.CompletionTokens,
			TotalTokens:      d.Usage.TotalTokens,
		}
	}
	for _, ch := range d.Choices {
		if ch.Delta.Content != "" {
			r.content.WriteString(ch.Delta.Content)
		}
		for _, tc := range ch.Delta.ToolCalls {
			pt, ok := r.tools[tc.Index]
			if !ok {
				pt = &partialTool{}
				r.tools[tc.Index] = pt
				r.toolKeys = append(r.toolKeys, tc.Index)
			}
			if tc.ID != "" {
				pt.id = tc.ID
			}
			if tc.Function.Name != "" {
				pt.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				pt.args.WriteString(tc.Function.Arguments)
			}
		}
	}
}

// Finalize returns the assembled response. Safe to call multiple times.
func (r *sseReassembler) Finalize() (*protocol.ParsedResponse, error) {
	if r.parseErr != nil && r.id == "" && r.content.Len() == 0 && len(r.tools) == 0 {
		return nil, r.parseErr
	}
	if r.id == "" && r.content.Len() == 0 && len(r.tools) == 0 && r.usage == nil {
		return nil, errors.New("empty stream")
	}
	out := &protocol.ParsedResponse{
		RequestID: r.id,
		Model:     r.model,
	}
	if r.content.Len() > 0 {
		out.Events = append(out.Events, protocol.Event{
			Type:    protocol.EventAssistantMessage,
			Role:    roleAssistant,
			Content: append([]byte(nil), r.content.Bytes()...),
		})
	}
	for _, idx := range r.toolKeys {
		pt := r.tools[idx]
		out.Events = append(out.Events, protocol.Event{
			Type:       protocol.EventToolCall,
			Role:       roleAssistant,
			ToolName:   pt.name,
			ToolCallID: pt.id,
			Content:    append([]byte(nil), pt.args.Bytes()...),
		})
	}
	if r.usage != nil {
		out.Events = append(out.Events, protocol.Event{
			Type:  protocol.EventUsage,
			Usage: r.usage,
		})
	}
	return out, nil
}
