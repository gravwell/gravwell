package main

import (
	"fmt"
	"time"
)

// Event represents a single generated event.
type Event struct {
	Timestamp time.Time
	Message   string
}

// String returns the event formatted as a log line.
func (e Event) String() string {
	return fmt.Sprintf("%s %s", e.Timestamp.Format(time.RFC3339Nano), e.Message)
}

// GenerateEvents creates n events with sequential timestamps starting from now.
func GenerateEvents(n int) []Event {
	return GenerateEventsFrom(n, time.Now())
}

// GenerateEventsFrom creates n events with sequential timestamps starting from the given time.
func GenerateEventsFrom(n int, start time.Time) []Event {
	events := make([]Event, n)
	for i := range n {
		events[i] = Event{
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Message:   fmt.Sprintf("genawsdata test event %d of %d", i+1, n),
		}
	}
	return events
}
