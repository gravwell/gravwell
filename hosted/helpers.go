package hosted

import (
	"errors"
	"fmt"
	"time"

	"github.com/gravwell/gravwell/v3/hosted/storage"
)

// GetTimeOrDefault loads a time from rt.
// Returns fallback if the key is absent, zero, or in the future.
func GetTimeOrDefault(rt Runtime, key string, fallback time.Time) (time.Time, error) {
	ts, err := rt.GetTime(key)
	if err != nil {
		if errors.Is(err, storage.ErrStorageNotFound) {
			return fallback, nil
		}

		return time.Time{}, fmt.Errorf("get time: %w", err)
	}

	if ts.IsZero() || ts.After(time.Now()) {
		return fallback, nil
	}

	return ts, nil
}

// GetStringOrDefault loads a string from rt.
// Returns fallback if the key is absent.
func GetStringOrDefault(rt Runtime, key, fallback string) (string, error) {
	s, err := rt.GetString(key)
	if err != nil {
		if errors.Is(err, storage.ErrStorageNotFound) {
			return fallback, nil
		}

		return "", fmt.Errorf("get string: %w", err)
	}

	return s, nil
}
