/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// output in the shape `log stream --style json` produces, including the status line it prints first
const sampleStream = `Filtering the log data using "process == \"sudo\""
[{
  "traceID" : 1234,
  "eventMessage" : "first message with },{ and [ ] inside",
  "eventType" : "logEvent",
  "source" : null,
  "backtrace" : {
    "frames" : [
      {
        "imageOffset" : 5678,
        "imageUUID" : "4F0D3C6A-0000-0000-0000-000000000000"
      }
    ]
  },
  "processImagePath" : "\/usr\/bin\/sudo",
  "timestamp" : "2026-09-30 10:02:12.345678-0600",
  "processID" : 42
},{
  "traceID" : 5678,
  "eventMessage" : "second message",
  "eventType" : "logEvent",
  "timestamp" : "2026-09-30 10:02:13.000001-0600",
  "processID" : 43
}]
`

var sampleEvents = []string{
	`{"traceID":1234,"eventMessage":"first message with },{ and [ ] inside","eventType":"logEvent","source":null,"backtrace":{"frames":[{"imageOffset":5678,"imageUUID":"4F0D3C6A-0000-0000-0000-000000000000"}]},"processImagePath":"\/usr\/bin\/sudo","timestamp":"2026-09-30 10:02:12.345678-0600","processID":42}`,
	`{"traceID":5678,"eventMessage":"second message","eventType":"logEvent","timestamp":"2026-09-30 10:02:13.000001-0600","processID":43}`,
}

func decodeAll(t *testing.T, input string, maxSize int64) (events []string, preamble []string, err error) {
	t.Helper()
	dec := newEventDecoder(strings.NewReader(input), maxSize, func(line string) {
		preamble = append(preamble, line)
	})
	for {
		var b []byte
		if b, err = dec.Next(); err != nil {
			return
		}
		events = append(events, string(b))
	}
}

func checkEvents(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d:\n got %s\nwant %s", i, got[i], want[i])
		}
	}
}

func TestDecodeArray(t *testing.T) {
	events, preamble, err := decodeAll(t, sampleStream, maxEventSize)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	checkEvents(t, events, sampleEvents)
	if len(preamble) != 1 || !strings.HasPrefix(preamble[0], "Filtering the log data") {
		t.Errorf("unexpected preamble %q", preamble)
	}
}

func TestDecodeUnterminatedArray(t *testing.T) {
	// a live stream never closes the array, and a killed process can stop anywhere
	input := strings.TrimSuffix(sampleStream, "]\n")
	events, _, err := decodeAll(t, input, maxEventSize)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	checkEvents(t, events, sampleEvents)

	head, _, _ := strings.Cut(sampleStream, `"second message"`)
	events, _, err = decodeAll(t, head, maxEventSize)
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("expected io.ErrUnexpectedEOF, got %v", err)
	}
	checkEvents(t, events, sampleEvents[:1])
}

func TestDecodeNDJSON(t *testing.T) {
	input := "\n" + sampleEvents[0] + "\n" + sampleEvents[1] + "\n"
	events, preamble, err := decodeAll(t, input, maxEventSize)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	checkEvents(t, events, sampleEvents)
	if len(preamble) != 0 {
		t.Errorf("unexpected preamble %q", preamble)
	}
}

func TestDecodeEmpty(t *testing.T) {
	for _, input := range []string{``, "\n\n", "[]", "Filtering the log data\n"} {
		events, _, err := decodeAll(t, input, maxEventSize)
		if err != io.EOF {
			t.Errorf("%q: expected io.EOF, got %v", input, err)
		}
		checkEvents(t, events, nil)
	}
}

func TestDecodeOversizedEvent(t *testing.T) {
	// an unterminated string must not be buffered forever
	input := `[{"eventMessage":"` + strings.Repeat("A", 4096)
	_, _, err := decodeAll(t, input, 1024)
	if !errors.Is(err, errEventTooLarge) {
		t.Fatalf("expected errEventTooLarge, got %v", err)
	}

	// events under the limit are fine, and the limit applies to each event rather than the stream
	var sb strings.Builder
	sb.WriteString("[")
	for i := range 100 {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(sampleEvents[1])
	}
	events, _, err := decodeAll(t, sb.String(), 1024)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	} else if len(events) != 100 {
		t.Fatalf("got %d events, want 100", len(events))
	}
}

func TestDecodeOversizedPreamble(t *testing.T) {
	input := strings.Repeat("not json at all\n", maxPreambleSize/8)
	if _, _, err := decodeAll(t, input, maxEventSize); !errors.Is(err, errPreambleTooLarge) {
		t.Fatalf("expected errPreambleTooLarge, got %v", err)
	}
	// a single enormous line without a newline hits the same limit
	input = strings.Repeat("x", maxPreambleSize*2)
	if _, _, err := decodeAll(t, input, maxEventSize); !errors.Is(err, errPreambleTooLarge) {
		t.Fatalf("expected errPreambleTooLarge, got %v", err)
	}
}

func TestDecodeGarbage(t *testing.T) {
	// cut on the separator between elements, not the one inside the first message
	head, _, _ := strings.Cut(sampleStream, "\n},{")
	events, _, err := decodeAll(t, head+"\n}, oops", maxEventSize)
	if err == nil || err == io.EOF {
		t.Fatalf("expected a syntax error, got %v", err)
	}
	checkEvents(t, events, sampleEvents[:1])
}

const realWorldSample = `[{
  "timezoneName" : "",
  "eventType" : "activityCreateEvent",
  "formatString" : "",
  "userID" : 0,
  "activityIdentifier" : 4267499,
  "subsystem" : "",
  "category" : "",
  "threadID" : 1801340,
  "senderImageUUID" : "97898286-081F-30D5-B1EB-65EADA074AE2",
  "backtrace" : {
    "frames" : [
      {
        "imageOffset" : 175268,
        "imageUUID" : "97898286-081F-30D5-B1EB-65EADA074AE2"
      }
    ]
  },
  "bootUUID" : "",
  "processImagePath" : "\/usr\/libexec\/runningboardd",
  "senderImagePath" : "\/System\/Library\/PrivateFrameworks\/RunningBoard.framework\/Versions\/A\/RunningBoard",
  "timestamp" : "2026-10-15 12:56:43.643291-0400",
  "creatorActivityID" : 0,
  "machTimestamp" : 4385161091565,
  "eventMessage" : "acquireAssertionWithDescriptor",
  "processImageUUID" : "EF79FF99-784F-3477-9AD6-DFF7F007EFF2",
  "traceID" : 4703001829209014530,
  "processID" : 410,
  "senderProgramCounter" : 175268,
  "parentActivityIdentifier" : 0
},{
  "timezoneName" : "",
  "eventType" : "activityCreateEvent",
  "formatString" : "",
  "userID" : 0,
  "activityIdentifier" : 4267500,
  "subsystem" : "",
  "category" : "",
  "threadID" : 1801339,
  "senderImageUUID" : "97898286-081F-30D5-B1EB-65EADA074AE2",
  "backtrace" : {
    "frames" : [
      {
        "imageOffset" : 56184,
        "imageUUID" : "97898286-081F-30D5-B1EB-65EADA074AE2"
      }
    ]
  },
  "bootUUID" : "",
  "processImagePath" : "\/usr\/libexec\/runningboardd",
  "senderImagePath" : "\/System\/Library\/PrivateFrameworks\/RunningBoard.framework\/Versions\/A\/RunningBoard",
  "timestamp" : "2026-10-15 12:56:43.647892-0400",
  "creatorActivityID" : 0,
  "machTimestamp" : 4385161201984,
  "eventMessage" : "state update",
  "processImageUUID" : "EF79FF99-784F-3477-9AD6-DFF7F007EFF2",
  "traceID" : 4702937735446724866,
  "processID" : 410,
  "senderProgramCounter" : 56184,
  "parentActivityIdentifier" : 4267499
}
`

var realWorldEvents = []string{
	`{"timezoneName":"","eventType":"activityCreateEvent","formatString":"","userID":0,"activityIdentifier":4267499,"subsystem":"","category":"","threadID":1801340,"senderImageUUID":"97898286-081F-30D5-B1EB-65EADA074AE2","backtrace":{"frames":[{"imageOffset":175268,"imageUUID":"97898286-081F-30D5-B1EB-65EADA074AE2"}]},"bootUUID":"","processImagePath":"\/usr\/libexec\/runningboardd","senderImagePath":"\/System\/Library\/PrivateFrameworks\/RunningBoard.framework\/Versions\/A\/RunningBoard","timestamp":"2026-10-15 12:56:43.643291-0400","creatorActivityID":0,"machTimestamp":4385161091565,"eventMessage":"acquireAssertionWithDescriptor","processImageUUID":"EF79FF99-784F-3477-9AD6-DFF7F007EFF2","traceID":4703001829209014530,"processID":410,"senderProgramCounter":175268,"parentActivityIdentifier":0}`,
	`{"timezoneName":"","eventType":"activityCreateEvent","formatString":"","userID":0,"activityIdentifier":4267500,"subsystem":"","category":"","threadID":1801339,"senderImageUUID":"97898286-081F-30D5-B1EB-65EADA074AE2","backtrace":{"frames":[{"imageOffset":56184,"imageUUID":"97898286-081F-30D5-B1EB-65EADA074AE2"}]},"bootUUID":"","processImagePath":"\/usr\/libexec\/runningboardd","senderImagePath":"\/System\/Library\/PrivateFrameworks\/RunningBoard.framework\/Versions\/A\/RunningBoard","timestamp":"2026-10-15 12:56:43.647892-0400","creatorActivityID":0,"machTimestamp":4385161201984,"eventMessage":"state update","processImageUUID":"EF79FF99-784F-3477-9AD6-DFF7F007EFF2","traceID":4702937735446724866,"processID":410,"senderProgramCounter":56184,"parentActivityIdentifier":4267499}`,
}

func TestDecodeRealWorldSample(t *testing.T) {
	events, _, err := decodeAll(t, realWorldSample, maxEventSize)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	checkEvents(t, events, realWorldEvents)

	for i, raw := range events {
		if _, err := eventTimestamp([]byte(raw)); err != nil {
			t.Errorf("event %d: failed to parse timestamp: %v", i, err)
		}
	}
}

func TestEventTimestamp(t *testing.T) {
	want := time.Date(2026, 9, 30, 16, 2, 12, 345678000, time.UTC)
	ts, err := eventTimestamp([]byte(sampleEvents[0]))
	if err != nil {
		t.Fatal(err)
	} else if !ts.Equal(want) {
		t.Fatalf("got %v, want %v", ts, want)
	}

	for _, v := range []string{
		`{"timestamp":"2026-09-30 10:02:12-0600"}`,
		`{"timestamp":"2026-09-30 10:02:12.345678123-0600"}`,
		`{"timestamp":"2026-09-30 16:02:12.3Z"}`,
	} {
		if _, err := eventTimestamp([]byte(v)); err != nil {
			t.Errorf("%s: %v", v, err)
		}
	}

	for _, v := range []string{
		`{"eventMessage":"no timestamp"}`,
		`{"timestamp":"yesterday"}`,
		`{"timestamp":12345}`,
		`[1,2,3]`,
	} {
		if _, err := eventTimestamp([]byte(v)); err == nil {
			t.Errorf("%s: expected an error", v)
		}
	}
}
