/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

// NOTE ON SCOPE: main.go's real work (connecting, receiving, checkpointing)
// happens through closures built inside main() around concrete Azure SDK
// clients (*eventhubs.ConsumerClient, *eventhubs.ProcessorPartitionClient),
// which aren't mockable without introducing interfaces main.go doesn't
// otherwise need. These tests cover the pieces of logic that were pulled out
// into standalone functions specifically because they're the parts most
// likely to have bugs (connection-string formatting, start-position choice,
// timestamp fallback ordering) and because they're the only pieces that can
// be tested without a live Event Hub and Storage account.

func TestBuildEventHubConnectionString(t *testing.T) {
	hubDef := eventHubConf{
		Event_Hubs_Namespace: "myNamespace",
		Event_Hub:            "myHub",
		Token_Name:           "myPolicy",
		Token_Key:            "s3cr3t==",
	}

	got := buildEventHubConnectionString(hubDef)
	want := "Endpoint=sb://myNamespace.servicebus.windows.net/;SharedAccessKeyName=myPolicy;SharedAccessKey=s3cr3t==;EntityPath=myHub"

	if got != want {
		t.Errorf("buildEventHubConnectionString() = %q, want %q", got, want)
	}
}

func TestStartPositionFor(t *testing.T) {
	cases := []struct {
		name       string
		checkpoint string
		wantEarly  bool // true if we expect Earliest set, false if Latest set
	}{
		{"empty defaults to start", "", true},
		{"explicit start", "start", true},
		{"explicit end", "end", false},
		{"unrecognized value defaults to start", "garbage", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pos := startPositionFor(tc.checkpoint)

			gotEarliest := pos.Earliest != nil && *pos.Earliest
			gotLatest := pos.Latest != nil && *pos.Latest

			if tc.wantEarly {
				if !gotEarliest {
					t.Errorf("startPositionFor(%q): expected Earliest=true, got %+v", tc.checkpoint, pos)
				}
				if gotLatest {
					t.Errorf("startPositionFor(%q): did not expect Latest set, got %+v", tc.checkpoint, pos)
				}
			} else {
				if !gotLatest {
					t.Errorf("startPositionFor(%q): expected Latest=true, got %+v", tc.checkpoint, pos)
				}
				if gotEarliest {
					t.Errorf("startPositionFor(%q): did not expect Earliest set, got %+v", tc.checkpoint, pos)
				}
			}
		})
	}
}

func TestEntryTimestamp_ParseTimeDisabled_UsesEnqueuedTime(t *testing.T) {
	hubDef := &eventHubConf{Parse_Time: false}
	enqueued := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)

	ts := entryTimestamp(hubDef, nil, []byte("irrelevant"), &enqueued)

	// Derive the expected value via entry.FromStandard itself (the same
	// function fallbackTimestamp calls) rather than computing it by hand:
	// entry.Timestamp does not store plain Unix seconds internally, so a
	// hand-rolled conversion would compare against the wrong representation.
	want := entry.FromStandard(enqueued)
	if ts.Sec != want.Sec || ts.Nsec != want.Nsec {
		t.Errorf("entryTimestamp() = %+v, want %+v", ts, want)
	}
	if hubDef.Parse_Time {
		// sanity: disabling parsing up front should not get flipped back on
		t.Errorf("Parse_Time changed unexpectedly: got %v", hubDef.Parse_Time)
	}
}

func TestEntryTimestamp_ParseTimeDisabled_NoEnqueuedTime_UsesNow(t *testing.T) {
	hubDef := &eventHubConf{Parse_Time: false}
	before := entry.FromStandard(time.Now())

	ts := entryTimestamp(hubDef, nil, []byte("irrelevant"), nil)

	after := entry.FromStandard(time.Now())
	if ts.Sec < before.Sec || ts.Sec > after.Sec {
		t.Errorf("entryTimestamp() with no enqueued time should fall back to now; got Sec=%d, expected between %d and %d",
			ts.Sec, before.Sec, after.Sec)
	}
}

func TestEntryTimestamp_ParseTimeEnabled_ExtractsFromBody(t *testing.T) {
	tg, err := timegrinder.NewTimeGrinder(timegrinder.Config{EnableLeftMostSeed: true})
	if err != nil {
		t.Fatalf("failed to build timegrinder: %v", err)
	}

	hubDef := &eventHubConf{Parse_Time: true}
	enqueued := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) // should NOT be used

	data := []byte("2024-03-04T05:06:07Z some log line")
	ts := entryTimestamp(hubDef, tg, data, &enqueued)

	want := entry.FromStandard(time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC))
	if ts.Sec != want.Sec || ts.Nsec != want.Nsec {
		t.Errorf("entryTimestamp() extracted %+v, want %+v", ts, want)
	}
	if !hubDef.Parse_Time {
		t.Errorf("Parse_Time should remain true after a successful extraction")
	}
}

func TestEntryTimestamp_ParseTimeEnabled_FailsExtraction_FallsBackAndDisables(t *testing.T) {
	tg, err := timegrinder.NewTimeGrinder(timegrinder.Config{EnableLeftMostSeed: true})
	if err != nil {
		t.Fatalf("failed to build timegrinder: %v", err)
	}

	hubDef := &eventHubConf{Parse_Time: true}
	enqueued := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)

	data := []byte("no timestamp in here at all")
	ts := entryTimestamp(hubDef, tg, data, &enqueued)

	want := entry.FromStandard(enqueued)
	if ts.Sec != want.Sec || ts.Nsec != want.Nsec {
		t.Errorf("entryTimestamp() on failed extraction = %+v, want fallback %+v", ts, want)
	}
	if hubDef.Parse_Time {
		t.Errorf("Parse_Time should be disabled after a failed extraction, so later events on this hub skip the (expensive, and apparently unhelpful) extraction attempt")
	}
}

func TestDebugout(t *testing.T) {
	origDebugOn := debugOn
	defer func() { debugOn = origDebugOn }()

	captureStdout := func(fn func()) string {
		old := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w
		fn()
		w.Close()
		os.Stdout = old

		var buf bytes.Buffer
		if _, err := io.Copy(&buf, r); err != nil {
			t.Fatalf("failed to read captured stdout: %v", err)
		}
		return buf.String()
	}

	debugOn = false
	out := captureStdout(func() { debugout("hello %s\n", "world") })
	if out != "" {
		t.Errorf("debugout() with debugOn=false printed %q, want nothing", out)
	}

	debugOn = true
	out = captureStdout(func() { debugout("hello %s\n", "world") })
	if out != "hello world\n" {
		t.Errorf("debugout() with debugOn=true printed %q, want %q", out, "hello world\n")
	}
}
