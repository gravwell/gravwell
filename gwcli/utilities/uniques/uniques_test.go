/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package uniques_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/group"
	. "github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	. "github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/cobra"
)

// NOTE(rlandau): these tests are limited as the validator generally only checks the last rune/word.
func TestCronRuneValidator(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		wantErr bool
	}{
		{"whitespace string", "     	", false},
		{"single letter", "a", true},
		{"random letters", randomdata.SillyName(), true},
		{"too many values", "1 2 3 4 5 6", true},
		{"last word too long", "1 2 3 4 555", true},
		{"all stars", "* * * * *", false},
		{"one star", "*", false},
		{"two stars", " * * ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CronRuneValidator(tt.arg); (err != nil) != tt.wantErr {
				t.Errorf("CronRuneValidator() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseJWT(t *testing.T) {
	// a valid, but outdated, token to use for testing
	validTkn := "7b22616c676f223a36323733382c22747970223a226a7774227d.7b22756964223a312c2265787069726573223a22323032352d30362d31305432323a34313a31312e3532353838333730325a222c22696174223a5b3139382c32332c39342c3232332c32342c34372c3134372c3139302c35382c3231342c3135362c3137362c3234342c3131302c35322c37352c3137322c3231372c3139382c3231352c3130352c3139302c342c3230352c38342c3130362c39352c3233332c3131322c3130362c31302c3133305d2c226e6f4c6f67696e4368616e6765223a66616c73652c226e6f44697361626c654d4641223a66616c73657d.526de9ffa6a7950c9812c3d378b9d8bc873b60770e485d041a33560f248579ece40f38b6bf84363dd4724cf14f735cdb3a120b414b7a6003dbe855a7f0bc3b45"
	// values known to be contained in the above JWT for validation purposes
	var expectedHeader = JWTHeader{
		Algo: 62738,
		Typ:  "jwt",
	}
	expectedTime, err := time.Parse("2006-01-02 15:04:05.999999999 +0000 UTC", "2025-06-10 22:41:11.525883702 +0000 UTC")
	if err != nil {
		t.Fatal(err)
	}
	var expectedPayload = JWTPayload{
		UID:     1,
		Expires: expectedTime,
	}

	if hdr, payload, sig, err := ParseJWT(validTkn); err != nil {
		t.Fatal("unexpected error:", ExpectedActual(nil, err))
	} else if hdr.Algo != expectedHeader.Algo || hdr.Typ != expectedHeader.Typ {
		t.Fatal("header mismatch:", ExpectedActual(expectedHeader, hdr))
	} else if payload.UID != expectedPayload.UID || payload.Expires != expectedPayload.Expires {
		t.Fatal("payload mismatch:", ExpectedActual(expectedPayload, payload))
	} else if sig == nil {
		t.Fatalf("nil signature")
	}
}

type ExpectedWalkResult struct {
	commandName     string
	remainingTokens []string
	builtin         string
	err             bool
}

func TestWalk(t *testing.T) {
	// build a tree to walk
	root := newNav("root", "short", "long", nil, []*cobra.Command{
		newNav("Anav", "short", "long", []string{"Anav_alias"}, nil),
		newNav("Bnav", "short", "long", nil, []*cobra.Command{
			newAction("BAaction", "short", "long", nil),
			newAction("BBaction", "short", "long", nil),
			newAction("BCaction", "short", "long", []string{"BCaction_alias1", "BCaction_alias2", "BCaction_alias3"}),
		}),
		newNav("Cnav", "short", "long", nil, []*cobra.Command{
			newAction("CAaction", "short", "long", nil),
			newAction("CBaction", "short", "long", nil),
			newNav("CCnav", "short", "long", []string{"CCnav_alias"}, []*cobra.Command{
				newAction("CCAaction", "short", "long", nil),
			}),
		}),
		newAction("Daction", "short", "long", nil),
	})

	builtins := []string{"builtin1", "builtin2", "help", "jump", "ls"}

	tests := []struct {
		name    string
		pwdPath string // if given, walks to this location and sets it as dir before passing in tokens
		input   string // string to tokenize and feed to walk
		want    ExpectedWalkResult
	}{
		{"first level nav", "", "Anav", ExpectedWalkResult{"Anav", nil, "", false}},
		{"first level nav alias", "", "Anav_alias", ExpectedWalkResult{"Anav", nil, "", false}},
		{"upward from root", "", "..", ExpectedWalkResult{"root", nil, "", false}},
		{"rootward from root", "", "~", ExpectedWalkResult{"root", nil, "", false}},
		{"rootward from root", "", "/", ExpectedWalkResult{"root", nil, "", false}},
		{"rootward", "Cnav", "/", ExpectedWalkResult{"root", nil, "", false}},
		{"unknown first token", "", "bad", ExpectedWalkResult{"root", nil, "", true}},
		{"second level action", "", "Bnav BCaction", ExpectedWalkResult{"BCaction", nil, "", false}},
		{"start at CCnav", "Cnav CCnav", "CCAaction", ExpectedWalkResult{"CCAaction", nil, "", false}},
		{"circuitous route", "Cnav CCnav", ".. .. Bnav ~ Cnav CBaction", ExpectedWalkResult{"CBaction", nil, "", false}},
		{"simple builtin", "", "builtin1", ExpectedWalkResult{"root", nil, "builtin1", false}},
		{"builtin with excess tokens", "", "builtin1 some extra tokens", ExpectedWalkResult{"root", []string{"some", "extra", "tokens"}, "builtin1", false}},
		{"interspersed builtin", "", "Bnav builtin1", ExpectedWalkResult{"Bnav", nil, "builtin1", false}},
		{"interspersed builtin", "", "Bnav builtin1 excess", ExpectedWalkResult{"Bnav", []string{"excess"}, "builtin1", false}},
		{"help builtin", "", "help", ExpectedWalkResult{"root", nil, "help", false}},
		{"help builtin, extra token", "", "help Anav", ExpectedWalkResult{"root", []string{"Anav"}, "help", false}},
		{"interspersed help", "", "Cnav help CCnav", ExpectedWalkResult{"Cnav", []string{"CCnav"}, "help", false}},
		{"interspersed help", "", "Cnav help CCnav CCAaction", ExpectedWalkResult{"Cnav", []string{"CCnav", "CCAaction"}, "help", false}},
		{"interspersed help", "", "Cnav CCnav help CCAaction", ExpectedWalkResult{"CCnav", []string{"CCAaction"}, "help", false}},
		// TODO -h flag
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startingDir := root
			// walk to pwd, if given
			if tt.pwdPath != "" {
				if c, tkns, err := root.Find(strings.Split(strings.TrimSpace(tt.pwdPath), " ")); err != nil {
					t.Fatal(err)
				} else if len(tkns) > 0 {
					t.Error("found extra tokens: ", tkns)
				} else {
					startingDir = c
				}
			}

			actual, err := Walk(startingDir, tt.input, builtins)
			if err := testWalkResult(actual, err, tt.want); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// helper for TestWalk
func newNav(use, short, long string, aliases []string, children []*cobra.Command) *cobra.Command {
	root := &cobra.Command{
		Use:     use,
		Short:   strings.ToLower(short),
		Long:    long,
		Aliases: aliases,
		GroupID: group.NavID,
		Run:     func(cmd *cobra.Command, args []string) {},
	}
	group.AddNavGroup(root)
	group.AddActionGroup(root)

	root.AddCommand(children...)

	return root
}

// helper for TestWalk
func newAction(use, short, long string, aliases []string) *cobra.Command {
	root := &cobra.Command{
		Use:     use,
		Short:   strings.ToLower(short),
		Long:    long,
		Aliases: aliases,
		GroupID: group.ActionID,
		Run:     func(cmd *cobra.Command, args []string) {},
	}

	return root
}

// helper for TestWalk
func testWalkResult(actual WalkResult, actualErr error, want ExpectedWalkResult) error {
	// check errors first
	if (want.err && actualErr == nil) || (!want.err && actualErr != nil) {
		return fmt.Errorf("mismatch error state.\nwant err? %v | actual err: %v", want.err, actualErr)
	}
	if actual.EndCmd != nil && (actual.EndCmd.Name() != want.commandName) {
		return errors.New(ExpectedActual(want.commandName, actual.EndCmd.Name()))
	}
	if slices.Compare(actual.RemainingTokens, want.remainingTokens) != 0 {
		return errors.New("bad remaining tokens" + ExpectedActual(want.remainingTokens, actual.RemainingTokens))
	}
	if actual.Builtin != want.builtin {
		return errors.New("bad built-in." + ExpectedActual(want.builtin, actual.Builtin))
	}

	return nil
}
