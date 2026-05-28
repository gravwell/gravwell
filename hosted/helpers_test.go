package hosted

import (
	"testing"
	"time"
)

func TestGetTimeOrDefault_MissingKey(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})
	fallback := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	got, err := GetTimeOrDefault(rt, "missing", fallback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Equal(fallback) {
		t.Errorf("expected fallback %v, got %v", fallback, got)
	}
}

func TestGetTimeOrDefault_ValidStoredTime(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})
	stored := time.Now().Add(-time.Hour).Truncate(time.Second)
	rt.PutTime("ts", stored) //nolint:errcheck

	got, err := GetTimeOrDefault(rt, "ts", time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Equal(stored) {
		t.Errorf("expected %v, got %v", stored, got)
	}
}

func TestGetTimeOrDefault_ZeroTimeReturnsFallback(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})
	rt.PutTime("ts", time.Time{}) //nolint:errcheck

	fallback := time.Now().Add(-time.Hour)
	got, err := GetTimeOrDefault(rt, "ts", fallback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Equal(fallback) {
		t.Errorf("zero stored time: expected fallback %v, got %v", fallback, got)
	}
}

func TestGetTimeOrDefault_FutureTimeReturnsFallback(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})
	rt.PutTime("ts", time.Now().Add(time.Hour)) //nolint:errcheck

	fallback := time.Now().Add(-time.Hour)
	got, err := GetTimeOrDefault(rt, "ts", fallback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Equal(fallback) {
		t.Errorf("future stored time: expected fallback %v, got %v", fallback, got)
	}
}

// TestGetTimeOrDefault_PropagatesNonNotFoundError verifies that a storage error
// other than ErrStorageNotFound is propagated as a wrapped error with a zero
// time value (not the fallback).
func TestGetTimeOrDefault_PropagatesNonNotFoundError(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})
	// corrupt the stored value so time.Parse fails
	rt.Put("ts", []byte("not-a-time")) //nolint:errcheck

	fallback := time.Now().Add(-time.Hour)
	got, err := GetTimeOrDefault(rt, "ts", fallback)
	if err == nil {
		t.Error("expected error for unparseable time, got nil")
	}
	// on non-NotFound error the return is zero time, not the fallback
	if !got.IsZero() {
		t.Errorf("expected zero time on error, got %v", got)
	}
}

func TestGetStringOrDefault_MissingKey(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})

	got, err := GetStringOrDefault(rt, "missing", "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "default" {
		t.Errorf("expected %q, got %q", "default", got)
	}
}

func TestGetStringOrDefault_StoredValue(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})
	rt.PutString("key", "hello") //nolint:errcheck

	got, err := GetStringOrDefault(rt, "key", "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("expected %q, got %q", "hello", got)
	}
}

func TestGetStringOrDefault_EmptyStoredValue(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})
	rt.PutString("key", "") //nolint:errcheck

	got, err := GetStringOrDefault(rt, "key", "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// empty string is a valid stored value, not a miss
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// TestGetStringOrDefault_PropagatesNonNotFoundError verifies that a storage
// error other than ErrStorageNotFound is propagated as a wrapped error with an
// empty string (not the fallback).
func TestGetStringOrDefault_PropagatesNonNotFoundError(t *testing.T) {
	ctx := t.Context()
	// Use a runtime whose GetString returns a non-NotFound error by injecting
	// a bad raw value that causes downstream failure — we simulate this by
	// overriding the store with a value that will trip the parser in GetTime,
	// but for strings we need a different approach. Use a wrappedErrRuntime.
	rt := &errStringRuntime{testRuntime: newTestRuntime(ctx, func() {})}

	got, err := GetStringOrDefault(rt, "key", "fallback")
	if err == nil {
		t.Error("expected error, got nil")
	}
	// on non-NotFound error the return is empty string, not the fallback
	if got != "" {
		t.Errorf("expected empty string on error, got %q", got)
	}
}

// errStringRuntime wraps testRuntime and returns a non-NotFound error from GetString.
type errStringRuntime struct {
	*testRuntime
}

func (r *errStringRuntime) GetString(_ string) (string, error) {
	return "", errStorageFailure
}

var errStorageFailure = &storageError{"simulated storage failure"}

type storageError struct{ msg string }

func (e *storageError) Error() string { return e.msg }
