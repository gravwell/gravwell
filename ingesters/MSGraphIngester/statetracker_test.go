package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestTracker(t *testing.T) *stateTracker {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	tracker, err := NewTracker(path, 48*time.Hour, nil)
	require.NoError(t, err)
	return tracker
}

func TestNewTracker(t *testing.T) {
	t.Parallel()

	t.Run("creates state file when absent", func(t *testing.T) {
		t.Parallel()
		tracker := newTestTracker(t)
		assert.NotNil(t, tracker)
		assert.NotNil(t, tracker.stateFout)
	})

	t.Run("invalid path returns error", func(t *testing.T) {
		t.Parallel()
		_, err := NewTracker("/nonexistent/path/state.db", 48*time.Hour, nil)
		assert.Error(t, err)
	})
}

func TestStateTracker_RecordAndExists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		seed    []string
		checkID string
		want    bool
	}{
		{
			name:    "unseen ID returns false",
			seed:    nil,
			checkID: "abc-123",
			want:    false,
		},
		{
			name:    "recorded ID returns true",
			seed:    []string{"abc-123"},
			checkID: "abc-123",
			want:    true,
		},
		{
			name:    "different ID returns false",
			seed:    []string{"abc-123"},
			checkID: "xyz-999",
			want:    false,
		},
		{
			name:    "multiple recorded IDs all return true",
			seed:    []string{"id-1", "id-2", "id-3"},
			checkID: "id-2",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tracker := newTestTracker(t)

			for _, id := range tt.seed {
				require.NoError(t, tracker.RecordId(id, time.Now()))
			}

			assert.Equal(t, tt.want, tracker.IdExists(tt.checkID))
		})
	}
}

func TestStateTracker_HorizonEviction(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.db")
	horizon := 1 * time.Hour
	tracker, err := NewTracker(path, horizon, nil)
	require.NoError(t, err)

	oldID := "old-alert"
	newID := "new-alert"

	// Record the old entry directly into the stateMap, bypassing the tempMap
	// so it survives the first tick and can be tested for eviction.
	tracker.Lock()
	tracker.stateMap[oldID] = time.Now().Add(-2 * time.Hour) // Older than the horizon.
	tracker.stateMap[newID] = time.Now()                     // Still within the horizon.
	tracker.Unlock()

	assert.True(t, tracker.IdExists(oldID), "%q should exist before eviction", oldID)
	assert.True(t, tracker.IdExists(newID), "%q should exist before eviction", newID)

	tracker.Lock()
	tracker.cleanStatesNoLock()
	tracker.Unlock()

	assert.False(t, tracker.IdExists(oldID), "%q should have been evicted after cleaning states", oldID)
	assert.True(t, tracker.IdExists(newID), "%q should have survived the state cleaning", newID)
}

func TestStateTracker_Persistence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	tracker1, err := NewTracker(path, 48*time.Hour, nil)
	require.NoError(t, err)

	persistedID := "persisted-id"

	require.NoError(t, tracker1.RecordId(persistedID, time.Now()))

	tracker1.Lock()
	// Flushes tempMap into stateMap and into disk (nil ingester means Sync is skipped).
	require.NoError(t, tracker1.tickNoLock())
	tracker1.Unlock()

	tracker2, err := NewTracker(path, 48*time.Hour, nil)
	require.NoError(t, err)

	assert.True(t, tracker2.IdExists(persistedID), "%q written by first tracker should be visible to second tracker")
	assert.False(t, tracker2.IdExists("never-recorded"), "unrecorded ID should not exist")
}

func TestStateTracker_TempMapPromotedOnTick(t *testing.T) {
	t.Parallel()

	tracker := newTestTracker(t)

	tempID := "temp-id"

	require.NoError(t, tracker.RecordId(tempID, time.Now()))

	tracker.Lock()
	// After tick w/ nil ingester, Sync is skipped but promotion should still happen.
	require.NoError(t, tracker.tickNoLock())
	tracker.Unlock()

	// After tick, the id should be promoted to stateMap.
	assert.True(t, tracker.IdExists(tempID))
	tracker.Lock()
	assert.Contains(t, tracker.stateMap, tempID)
	assert.NotContains(t, tracker.tempMap, tempID)
	tracker.Unlock()
}
