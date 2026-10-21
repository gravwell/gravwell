package hosted

import (
	"errors"
	"fmt"
	"time"

	"github.com/gravwell/gravwell/v4/hosted/storage"
)

// GetTimeOrDefault loads a time from s.
// Returns fallback if the key is absent, zero, or in the future.
func GetTimeOrDefault(s Storage, key string, fallback time.Time) (time.Time, error) {
	ts, err := s.GetTime(key)
	if err != nil {
		if errors.Is(err, storage.ErrStorageNotFound) {
			return fallback, nil
		}

		return time.Time{}, fmt.Errorf("unexpected error getting time: %w", err)
	}

	if ts.IsZero() || ts.After(time.Now()) {
		return fallback, nil
	}

	return ts, nil
}

// GetStringOrDefault loads a string from s.
// Returns fallback if the key is absent.
func GetStringOrDefault(s Storage, key, fallback string) (string, error) {
	str, err := s.GetString(key)
	if err != nil {
		if errors.Is(err, storage.ErrStorageNotFound) {
			return fallback, nil
		}

		return "", fmt.Errorf("unexpected error getting string: %w", err)
	}

	return str, nil
}
