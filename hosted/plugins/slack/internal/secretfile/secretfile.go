// Package secretfile provides the one-line secret-file contract shared by
// private Hosted Runner plugins.
package secretfile

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Read loads one non-empty secret, stripping one physical line ending. Other
// leading and trailing whitespace is preserved because it may be significant.
func Read(path, label string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%s must be specified", label)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s must reference a regular file", label)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	value := string(data)
	if strings.HasSuffix(value, "\r\n") {
		value = strings.TrimSuffix(value, "\r\n")
	} else if strings.HasSuffix(value, "\n") {
		value = strings.TrimSuffix(value, "\n")
	}
	if value == "" {
		return "", fmt.Errorf("%s is empty", label)
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", errors.New(label + " must contain exactly one physical line")
	}
	return value, nil
}

// Prefer loads path when configured and otherwise returns the legacy value.
// New examples use files exclusively; the fallback permits a deliberate,
// non-breaking migration of existing private configurations.
func Prefer(path, legacy, label string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return legacy, nil
	}
	return Read(path, label)
}
