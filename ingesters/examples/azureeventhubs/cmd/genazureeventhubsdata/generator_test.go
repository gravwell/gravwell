package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateEvents(t *testing.T) {
	events := GenerateEvents(5)
	require.Len(t, events, 5)

	for i, e := range events {
		assert.Contains(t, e.Message, "test event")
		assert.Contains(t, e.Message, "of 5")
		if i > 0 {
			assert.True(t, e.Timestamp.After(events[i-1].Timestamp),
				"event %d should be after event %d", i, i-1)
		}
	}
}

func TestGenerateEventsFrom(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	events := GenerateEventsFrom(3, start)

	require.Len(t, events, 3)
	assert.Equal(t, start, events[0].Timestamp)
	assert.Equal(t, start.Add(1*time.Second), events[1].Timestamp)
	assert.Equal(t, start.Add(2*time.Second), events[2].Timestamp)

	assert.Contains(t, events[0].Message, "1 of 3")
	assert.Contains(t, events[2].Message, "3 of 3")
}

func TestGenerateEventsZero(t *testing.T) {
	events := GenerateEvents(0)
	assert.Empty(t, events)
}

func TestEventString(t *testing.T) {
	e := Event{
		Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		Message:   "hello world",
	}
	s := e.String()
	assert.Contains(t, s, "2026-01-01T12:00:00Z")
	assert.Contains(t, s, "hello world")
}
