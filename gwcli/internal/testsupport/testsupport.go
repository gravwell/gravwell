/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package testsupport provides utility functions useful across disparate testing packages
//
// TT* functions are for use with tests that rely on TeaTest.
// Friendly reminder: calling tm.Type() with "\n"/"\t"/etc does not, at the time of writing, actually trigger the corresponding key message.
package testsupport

import (
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/spf13/pflag"
)

const (
	// This adds a short pause after TTSendSpecial sends.
	// This is because tea.Cmds are async and time-unbounded.
	// In other words, we need to buy Bubbletea extra time for the messages to propagate down to the final action model.
	// If we don't, MatchGolden can fail even if the final-final output is correct.
	SendSpecialPause time.Duration = 50 * time.Millisecond
)

//#region TeaTest

// A MessageRcvr is anything that can accept tea.Msgs via a .Send() method.
type MessageRcvr interface {
	Send(tea.Msg)
}

// TTSendSpecial submits a KeyMsg containing the special key (CTRL+C, ESC, etc) to the test model.
// Ensures the KeyMsg is well-formatted, as ill-formatted KeyMsgs are silently dropped (as they are not read as KeyMsgs) or cause panics.
//
// For use with TeaTests.
func TTSendSpecial(r MessageRcvr, kt tea.KeyType) {
	r.Send(tea.KeyMsg(tea.Key{Type: kt, Runes: []rune{rune(kt)}}))
	time.Sleep(SendSpecialPause)

}

// Type adds teatest.TestModel.Type() to a normal tea.Program.
func Type(prog *tea.Program, text string) {
	for _, r := range text {
		prog.Send(tea.KeyMsg(
			tea.Key{Type: tea.KeyRunes, Runes: []rune{rune(r)}}))
	}
}

// TypeUpdate sends each character of text into the given update function, one by one.
func TypeUpdate(update func(msg tea.Msg) tea.Cmd, text string) {
	for _, r := range text {
		update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// TTMatchGolden is a convenience function to check FinalOutput or Output of tm against its golden.
//
// ! If final, this blocks until tm returns.
func TTMatchGolden(t *testing.T, tm *teatest.TestModel, final bool, finalWait time.Duration) []byte {
	t.Helper()
	var o io.Reader
	if !final {
		o = tm.Output()
	} else {
		o = tm.FinalOutput(t, teatest.WithFinalTimeout(finalWait))
	}
	out, err := io.ReadAll(o)
	if err != nil {
		t.Error(err)
	}
	// matches on the golden file with the test function's name
	teatest.RequireEqualOutput(t, out)
	return out
}

//#endregion TeaTest

const ENV_SERVER string = "GWCLI_TEST_SERVER"

// Server attempts to pull the server string from the environment.
// Returns localhost:80 if the env var is unset or empty
func Server() string {
	if s, found := os.LookupEnv(ENV_SERVER); found {
		return s
	}
	return "localhost:80"
}

// ExpectedActual returns a string declaring what was expected and what we got instead.
// ! Prefixes the string with a newline.
func ExpectedActual(expected, actual any) string {
	return fmt.Sprintf("\n\tExpected:'%+v'\n\tGot:'%+v'", expected, actual)
}

// Uncloak replaces whitespace characters with visible representations.
//
// \t 		-> ↹ (U+21B9)
// \n 		-> ↵ (U+21B5) (newline will be retained, not replaced)
// (space) 	-> · (U+B7)
func Uncloak(s string) string {
	s = strings.ReplaceAll(s, " ", "·")
	s = strings.ReplaceAll(s, "\n", "↵\n")
	s = strings.ReplaceAll(s, "\t", "↹")
	return s
}

// NonZeroExit calls Fatal if code is <> 0.
func NonZeroExit(t *testing.T, code int, stderr string) {
	t.Helper()
	if code != 0 {
		t.Fatalf("non-zero exit code %v.\nstderr: '%v'", code, stderr)
	}
}

// SlicesUnorderedEqual compares the elements of two slices for equality (and equal count) without caring about the order of the elements.
// Copied from my (rflandau) Orv test code.
func SlicesUnorderedEqual(a []string, b []string) bool {
	// convert each slice into map of key --> count
	am := make(map[string]uint)
	for _, k := range a {
		am[k] += 1
	}
	bm := make(map[string]uint)
	for _, k := range b {
		bm[k] += 1
	}

	return maps.Equal(am, bm)
}

// ExtractPrintLineMessageString attempts to pull the messageBody string out from the tea.printLineMessage private struct by reflecting into it.
// It can parse sequences/batches.
// Returns the string on success; fatal on failure.
// Only operates at the first layer; will not traverse nested sequence/batches
//
// If !sliceOK, then it will fail if the given command returned a tea.Batch or tea.Sequence.
// sequenceIndex sets the expected index of the printLineMessage if the cmd is a tea.Batch or tea.Sequence.
// Has no effect if !sliceOK.
func ExtractPrintLineMessageString(t *testing.T, cmd tea.Cmd, sliceOK bool, sequenceIndex uint) string {
	t.Helper()
	voMsg := reflect.ValueOf(cmd())
	//t.Logf("Update msg kind: %v", voMsg.Kind())
	// this will be a slice if it is a sequence or a struct if a single msg
	var voPLM reflect.Value
	if voMsg.Kind() == reflect.Slice {
		if !sliceOK {
			t.Fatal("message is a slice; slices were marked unacceptable")
		}
		// ensure the sequence/batch is at least as large as the index
		if voMsg.Len() <= int(sequenceIndex) {
			t.Fatal("sequence/batch is too short.", ExpectedActual(fmt.Sprintf("at least %v", sequenceIndex), voMsg.Len()))
		}
		// select a single item
		voInnerCmd := voMsg.Index(int(sequenceIndex))
		// voItm1 should now be a Cmd that returns a printLineMessage
		if voInnerCmd.Kind() != reflect.Func {
			t.Fatal(ExpectedActual(reflect.Func, voMsg.Kind()))
		}
		// invoke, check that exactly 1 value (the message) is returned
		if voInnerMsg := voInnerCmd.Call(nil); len(voInnerMsg) != 1 {
			t.Fatal("bad output count", ExpectedActual(1, len(voInnerMsg)))
		} else {
			voPLM = voInnerMsg[sequenceIndex]
		}
	} else { // not a sequence, just a raw printLineMessage (or an interface of a Msg)
		voPLM = voMsg
	}

	// if the Message is still in interface form, we need to dereference it
	if voPLM.Kind() == reflect.Interface {
		voPLM = voPLM.Elem()
	}
	if voPLM.Kind() != reflect.Struct {
		t.Fatal(ExpectedActual(reflect.Struct, voPLM.Kind()))
	}

	voMessageBody := voPLM.FieldByName("messageBody")
	if voMessageBody.Kind() != reflect.String {
		t.Fatal(ExpectedActual(reflect.String, voMessageBody.Kind()))
	}
	return voMessageBody.String()
}

// CheckSetArgs calls SetArgs on the given Model and tests that its return values are as expected.
//
// Calls fatal on failure.
func CheckSetArgs(t *testing.T,
	setArgsFunc func(parentFS *pflag.FlagSet, tokens []string, width int, height int) (invalid string, onStart tea.Cmd, err error),
	flagset *pflag.FlagSet, tokens []string, width, height int,
	wantInvalid bool, wantOnStart tea.Cmd, wantErr bool) {
	t.Helper()
	invalid, onStart, err := setArgsFunc(flagset, tokens, width, height)

	if (invalid != "") != wantInvalid || !reflect.DeepEqual(onStart, wantOnStart) || (err != nil) != wantErr {
		t.Fatal("bad SetArgs results."+
			"\nWantInvalid? ", wantInvalid, " | Invalid: ", invalid,
			"\nonStart:", ExpectedActual(wantOnStart, onStart),
			"\nWantErr?", wantErr, " | err:", err)
	}
}

// LinesTrimSpace calls strings.TrimSpace on each line of the given string, allowing multiline strings to be compared white-space-agnostic.
func LinesTrimSpace(v string) string {
	var sb strings.Builder
	for line := range strings.SplitSeq(v, "\n") {
		sb.WriteString(strings.TrimSpace(line) + "\n")
	}

	return sb.String()
}

// SendHotkey converts a key.Binding into a tea.KeyMsg.
//
// Sends the first key in a binding, ignoring any others.
//
// ! Aimed at sending the bindings defined in hotkeys; this is a best-effort helper function.
func SendHotkey(b key.Binding) tea.KeyMsg {
	keys := b.Keys()
	if len(keys) == 0 {
		return tea.KeyMsg{}
	}
	msg := tea.KeyMsg{}

	// check for and trim off alt prefix
	k, altFound := strings.CutPrefix(keys[0], "alt+")
	if altFound {
		msg.Alt = true
	}

	// attempt a direct lookup
	if t, found := keyByName[strings.ToLower(k)]; found {
		msg.Type = t
		return msg
	}

	// if we didn't find it via direct, just assume it is arbitrary runes

	msg.Type = tea.KeyRunes
	msg.Runes = []rune(k)

	return msg
}

var keyByName = map[string]tea.KeyType{
	// Control keys.
	"ctrl+@":    tea.KeyCtrlAt,
	"ctrl+a":    tea.KeyCtrlA,
	"ctrl+b":    tea.KeyCtrlB,
	"ctrl+c":    tea.KeyCtrlC,
	"ctrl+d":    tea.KeyCtrlD,
	"ctrl+e":    tea.KeyCtrlE,
	"ctrl+f":    tea.KeyCtrlF,
	"ctrl+g":    tea.KeyCtrlG,
	"ctrl+h":    tea.KeyCtrlH,
	"tab":       tea.KeyTab,
	"ctrl+j":    tea.KeyCtrlJ,
	"ctrl+k":    tea.KeyCtrlK,
	"ctrl+l":    tea.KeyCtrlL,
	"enter":     tea.KeyEnter,
	"ctrl+n":    tea.KeyCtrlN,
	"ctrl+o":    tea.KeyCtrlO,
	"ctrl+p":    tea.KeyCtrlP,
	"ctrl+q":    tea.KeyCtrlQ,
	"ctrl+r":    tea.KeyCtrlR,
	"ctrl+s":    tea.KeyCtrlS,
	"ctrl+t":    tea.KeyCtrlT,
	"ctrl+u":    tea.KeyCtrlU,
	"ctrl+v":    tea.KeyCtrlV,
	"ctrl+w":    tea.KeyCtrlW,
	"ctrl+x":    tea.KeyCtrlX,
	"ctrl+y":    tea.KeyCtrlY,
	"ctrl+z":    tea.KeyCtrlZ,
	"esc":       tea.KeyEsc,
	"ctrl+\\":   tea.KeyCtrlBackslash,
	"ctrl+]":    tea.KeyCtrlCloseBracket,
	"ctrl+^":    tea.KeyCtrlCaret,
	"ctrl+_":    tea.KeyCtrlUnderscore,
	"backspace": tea.KeyBackspace,

	// Other keys.
	"runes":            tea.KeyRunes,
	"up":               tea.KeyUp,
	"down":             tea.KeyDown,
	"right":            tea.KeyRight,
	" ":                tea.KeySpace,
	"left":             tea.KeyLeft,
	"shift+tab":        tea.KeyShiftTab,
	"home":             tea.KeyHome,
	"end":              tea.KeyEnd,
	"ctrl+home":        tea.KeyCtrlHome,
	"ctrl+end":         tea.KeyCtrlEnd,
	"shift+home":       tea.KeyShiftHome,
	"shift+end":        tea.KeyShiftEnd,
	"ctrl+shift+home":  tea.KeyCtrlShiftHome,
	"ctrl+shift+end":   tea.KeyCtrlShiftEnd,
	"pgup":             tea.KeyPgUp,
	"pgdown":           tea.KeyPgDown,
	"ctrl+pgup":        tea.KeyCtrlPgUp,
	"ctrl+pgdown":      tea.KeyCtrlPgDown,
	"delete":           tea.KeyDelete,
	"insert":           tea.KeyInsert,
	"ctrl+up":          tea.KeyCtrlUp,
	"ctrl+down":        tea.KeyCtrlDown,
	"ctrl+right":       tea.KeyCtrlRight,
	"ctrl+left":        tea.KeyCtrlLeft,
	"shift+up":         tea.KeyShiftUp,
	"shift+down":       tea.KeyShiftDown,
	"shift+right":      tea.KeyShiftRight,
	"shift+left":       tea.KeyShiftLeft,
	"ctrl+shift+up":    tea.KeyCtrlShiftUp,
	"ctrl+shift+down":  tea.KeyCtrlShiftDown,
	"ctrl+shift+left":  tea.KeyCtrlShiftLeft,
	"ctrl+shift+right": tea.KeyCtrlShiftRight,
	"f1":               tea.KeyF1,
	"f2":               tea.KeyF2,
	"f3":               tea.KeyF3,
	"f4":               tea.KeyF4,
	"f5":               tea.KeyF5,
	"f6":               tea.KeyF6,
	"f7":               tea.KeyF7,
	"f8":               tea.KeyF8,
	"f9":               tea.KeyF9,
	"f10":              tea.KeyF10,
	"f11":              tea.KeyF11,
	"f12":              tea.KeyF12,
	"f13":              tea.KeyF13,
	"f14":              tea.KeyF14,
	"f15":              tea.KeyF15,
	"f16":              tea.KeyF16,
	"f17":              tea.KeyF17,
	"f18":              tea.KeyF18,
	"f19":              tea.KeyF19,
	"f20":              tea.KeyF20,
}
