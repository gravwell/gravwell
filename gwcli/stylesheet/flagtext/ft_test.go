//go:build ci

// Package ft_test is an almost redundant unit testing package.
package ft_test

import (
	"errors"
	"path"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/stretchr/testify/assert"
)

func TestDeriveFlagName(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want string
	}{
		{"unaffected", "input", "input"},
		{"replaced", `i\n.p/u"t v|a'lue`, "i-n-p-u-t-v-a-lue"},
		{"uppercase", "not SHOUTING", "not-shouting"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ft.DeriveFlagName(tt.arg); got != tt.want {
				t.Errorf("DeriveFlagName() = %v, want %v", got, tt.want)
			}
		})
	}
}

// A rather off-the-wall test to ensure that the brackets used to indicate flag semantics (mandatory, optional, mutually exclusive) are varied.
func TestUnequalBrackets(t *testing.T) {
	text := "some text"
	if m, o, me := ft.Mandatory(text), ft.Optional(text), ft.MutuallyExclusive([]string{text}); m == o || m == me || o == me {
		t.Fatalf("flag semantic brackets must differ. Got: Mandatory=%v|Optional=%v|Mutually Exclusive=%v", m, o, me)
	}
}

func TestErrMutuallyExclusive(t *testing.T) {
	logPth := path.Join(t.TempDir(), "cli.log")
	clilog.InitializeFromArgs([]string{"--" + clilog.FlagLogPath.Name + "=" + logPth})
	if err := ft.ErrMutuallyExclusive(); !errors.Is(err, clilog.ErrInternal{}) {
		t.Error("zero or one arguments should trigger an ErrInternal")
	}
	if err := ft.ErrMutuallyExclusive("one"); !errors.Is(err, clilog.ErrInternal{}) {
		t.Error("zero or one arguments should trigger an ErrInternal")
	}
	assert.EqualError(t, ft.ErrMutuallyExclusive("one", "two", "three"), "--one, --two, and --three are mutually exclusive")
}
