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
	"net"

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/ingesters/llm_ingester/protocol"
)

// emitCtx bundles the per-request metadata shared by every entry emitted from
// that request.
type emitCtx struct {
	tag          entry.EntryTag
	pproc        *processors.ProcessorSet
	listenerName string
	protocolName string
	logMode      string
	logToolCalls bool
	logUsage     bool
	clientIP     net.IP
	sessionID    string
	newSession   bool
	upstreamCode int
	stream       bool
	requestID    string
	model        string
	durationMs   int64
	lg           ingest.Logger
}

// emitRequestEvents writes the request-side events that the listener's
// Log_Mode dictates.
func emitRequestEvents(ec *emitCtx, evs []protocol.Event) {
	switch ec.logMode {
	case logModeUserOnly:
		// The most recent user message, plus any tool results from the current
		// turn when tool logging is on (so the response.tool_call entries have
		// matching results). Assistant turns are never logged in this mode.
		latest := -1
		for i := len(evs) - 1; i >= 0; i-- {
			if evs[i].Type == protocol.EventUserMessage {
				latest = i
				break
			}
		}
		tail := tailStart(evs)
		for i, e := range evs {
			switch e.Type {
			case protocol.EventUserMessage:
				if i != latest {
					continue
				}
			case protocol.EventToolResult:
				if !ec.logToolCalls || i < tail {
					continue
				}
			default:
				continue
			}
			writeEvent(ec, e)
		}
	case logModeFullConversation:
		// Every message in the request body.
		for _, e := range evs {
			if e.Type == protocol.EventToolResult && !ec.logToolCalls {
				continue
			}
			writeEvent(ec, e)
		}
	default: // deltas
		// Emit only the new turn: latest user message, plus any tool_result
		// messages that came in after the previous assistant turn. The
		// simplest robust rule is to log everything from the last
		// non-assistant block at the tail (latest user msg + any tool_result
		// tail messages). For new sessions, also emit the system prompt.
		if ec.newSession {
			for _, e := range evs {
				if e.Type == protocol.EventSystemMessage {
					writeEvent(ec, e)
				}
			}
		}
		// Everything after the last assistant turn is new (the latest user
		// message plus any tool_result tail); everything before it was already
		// logged in a previous request.
		for _, e := range evs[tailStart(evs):] {
			if e.Type == protocol.EventToolResult && !ec.logToolCalls {
				continue
			}
			// The system prompt is emitted above for a new session and is
			// never new on a later request. On the first request of a session
			// there is no assistant turn yet, so tailStart returns 0 and the
			// tail still contains it — writing it here would duplicate the
			// whole prompt, which for a Claude Code session is tens of KB.
			if e.Type == protocol.EventSystemMessage {
				continue
			}
			writeEvent(ec, e)
		}
	}
}

// tailStart returns the index of the first event after the last assistant
// message — the events that arrived with this request and were not already
// logged as part of a previous one.
func tailStart(evs []protocol.Event) int {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == protocol.EventAssistantMessage {
			return i + 1
		}
	}
	return 0
}

// emitResponseEvents writes the response-side events (assistant message,
// tool calls, usage) honoring listener flags.
func emitResponseEvents(ec *emitCtx, evs []protocol.Event) {
	for _, e := range evs {
		switch e.Type {
		case protocol.EventAssistantMessage:
			if ec.logMode == logModeUserOnly {
				continue // user mode logs prompts, not replies
			}
		case protocol.EventToolCall:
			if !ec.logToolCalls {
				continue
			}
		case protocol.EventUsage:
			if !ec.logUsage {
				continue
			}
		}
		writeEvent(ec, e)
	}
}

func writeEvent(ec *emitCtx, ev protocol.Event) {
	send(ec, buildEntry(ec, ev))
}

// buildEntry constructs an entry.Entry with the standard EV set for this proxy.
func buildEntry(ec *emitCtx, ev protocol.Event) *entry.Entry {
	e := &entry.Entry{
		TS:   entry.Now(),
		SRC:  ec.clientIP,
		Tag:  ec.tag,
		Data: ev.Content,
	}
	addEV(ec, e, "event_type", ev.Type)
	if ev.Role != "" {
		addEV(ec, e, "role", ev.Role)
	}
	if ev.ToolName != "" {
		addEV(ec, e, "tool_name", ev.ToolName)
	}
	if ev.ToolCallID != "" {
		addEV(ec, e, "tool_call_id", ev.ToolCallID)
	}
	if ev.Usage != nil {
		addEV(ec, e, "prompt_tokens", ev.Usage.PromptTokens)
		addEV(ec, e, "completion_tokens", ev.Usage.CompletionTokens)
		addEV(ec, e, "total_tokens", ev.Usage.TotalTokens)
	}
	if ec.sessionID != "" {
		addEV(ec, e, "session_id", ec.sessionID)
	}
	if ec.requestID != "" {
		addEV(ec, e, "request_id", ec.requestID)
	}
	if ec.model != "" {
		addEV(ec, e, "model", ec.model)
	}
	addEV(ec, e, "protocol", ec.protocolName)
	addEV(ec, e, "listener", ec.listenerName)
	if ec.upstreamCode != 0 {
		addEV(ec, e, "upstream_status", int64(ec.upstreamCode))
	}
	if ec.durationMs > 0 {
		addEV(ec, e, "duration_ms", ec.durationMs)
	}
	addEV(ec, e, "stream", ec.stream)
	if ec.newSession {
		addEV(ec, e, "new_session", true)
	}
	return e
}

func addEV(ec *emitCtx, e *entry.Entry, name string, val interface{}) {
	if err := e.AddEnumeratedValueEx(name, val); err != nil {
		ec.lg.Warn("failed to add EV",
			log.KV("name", name),
			log.KVErr(err))
	}
}

func send(ec *emitCtx, e *entry.Entry) {
	if ec.pproc == nil {
		return
	}
	if err := ec.pproc.ProcessContext(e, context.Background()); err != nil {
		ec.lg.Warn("failed to ingest entry", log.KVErr(err))
	}
}
