// Package testingsupport is exactly as it sounds: supporting functions for use across the splintered testing packages.
// Should never be imported by non-"_test.go" files/magefiles.
package testingsupport

import (
	"fmt"
	"testing"
)

// NonZeroExit calls Fatal if code is <> 0.
func NonZeroExit(t *testing.T, code int, stderr string) {
	t.Helper()
	if code != 0 {
		t.Fatalf("non-zero exit code %v.\nstderr: '%v'", code, stderr)
	}
}

// ExpectedActual returns a string declaring what was expected and what we got instead.
// NOTE(rlandau): Prefixes the string with a newline.
func ExpectedActual(expected, actual any) string {
	return fmt.Sprintf("\n\tExpected:'%v'\n\tGot:'%v'", expected, actual)
}

// Verboseln only prints the given string if verbose mode is enabled in the testing run.
func Verboseln(s string) {
	if testing.Verbose() {
		fmt.Println(s)
	}
}
