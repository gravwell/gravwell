package e2e

import "testing"

// Fatal is a wrapper around testing.T.Fatal that captures the Gravwell instance logs as an artifact.
func Fatal(t *testing.T, args ...interface{}) {
	t.Helper()
	defer t.Fatal(args...)
	saveInstanceLogs(t)
}

// Fatalf is a wrapper around testing.T.Fatalf that captures the Gravwell instance logs as an artifact.
func Fatalf(t *testing.T, s string, args ...interface{}) {
	t.Helper()
	defer t.Fatalf(s, args...)
	saveInstanceLogs(t)
}
