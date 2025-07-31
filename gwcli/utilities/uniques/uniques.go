/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package uniques contains global constants and functions that must be referenced across multiple packages
// but cannot belong to any.
// ! Uniques does not import any local packages as to prevent import cycles.
package uniques

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/shlex"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
	"github.com/spf13/cobra"
)

const (
	// the string format the Gravwell client requires
	SearchTimeFormat string = "2006-01-02T15:04:05.999999999Z07:00"
	Version          string = "v0.8"
)

// CronRuneValidator provides a validator function for a TI intended to consume cron-like input.
// For efficiencies sake, it only evaluates the end rune.
// Checking the values of each complete word is delayed until connection.CreateScheduledSearch to
// save on cycles.
func CronRuneValidator(s string) error {
	// check for an empty TI
	if strings.TrimSpace(s) == "" {
		return nil
	}
	runes := []rune(s)

	// check that the latest input is a digit or space
	if char := runes[len(runes)-1]; !unicode.IsSpace(char) &&
		!unicode.IsDigit(rune(char)) && char != '*' {
		return errors.New("frequency can contain only digits or '*'")
	}

	// check that we do not have too many values
	exploded := strings.Split(s, " ")
	if len(exploded) > 5 {
		return errors.New("must be exactly 5 values")
	}

	// check that the newest word is <= 2 characters
	lastWord := []rune(exploded[len(exploded)-1])
	if len(lastWord) > 2 {
		return errors.New("each word is <= 2 digits")
	}

	return nil
}

// A JWTHeader holds the values from the first segment of a parsed JWT.
type JWTHeader struct {
	Algo int    `json:"algo"`
	Typ  string `json:"typ"`
}

// A JWTPayload holds the values from the second segment of a parsed JWT.
// Most importantly for our purposes, the payload contains the timestamp after which the JWT will have expired.
type JWTPayload struct {
	UID           int       `json:"uid"`
	Expires       time.Time `json:"expires"`
	Iat           []int     `json:"iat"`
	NoLoginChange bool      `json:"noLoginChange"`
	NoDisableMFA  bool      `json:"noDisableMFA"`
}

// ParseJWT does as it says on the tin.
// The given string is unmarshaled into 3 chunks (header, payload, signature) and returned.
func ParseJWT(tkn string) (header JWTHeader, payload JWTPayload, signature []byte, err error) {
	exploded := strings.Split(tkn, ".")
	if len(exploded) != 3 {
		return JWTHeader{}, JWTPayload{}, nil, ErrBadJWTLength
	}

	// header
	decodedUrl, err := hex.DecodeString(exploded[0])
	if err != nil {
		return JWTHeader{}, JWTPayload{}, nil, err
	}
	if err := json.Unmarshal(decodedUrl, &header); err != nil {
		return JWTHeader{}, JWTPayload{}, nil, err
	}

	// payload
	decodedUrl, err = hex.DecodeString(exploded[1])
	if err != nil {
		return header, JWTPayload{}, nil, err
	}
	if err := json.Unmarshal(decodedUrl, &payload); err != nil {
		return header, JWTPayload{}, nil, err
	}

	// signature
	sig, err := hex.DecodeString(exploded[2])
	if err != nil {
		return header, JWTPayload{}, nil, err
	}

	return header, payload, sig, err
}

// AttachPersistentFlags populates all persistent flags and attaches them to the given command.
// This subroutine should ONLY be used by Mother when building the root command or by test suites that omit Mother.
func AttachPersistentFlags(cmd *cobra.Command) {
	ft.NoInteractive.Register(cmd.PersistentFlags())
	cmd.PersistentFlags().StringP("username", "u", "", "login credential.")
	cmd.PersistentFlags().String("password", "", "login credential.")
	cmd.PersistentFlags().StringP("passfile", "p", "", "the path to a file containing your password")
	cmd.PersistentFlags().String("api", "", "log in via API key instead of credentials")

	cmd.MarkFlagsMutuallyExclusive("password", "passfile", "api")
	cmd.MarkFlagsMutuallyExclusive("api", "username")

	ft.NoColor.Register(cmd.PersistentFlags())
	cmd.PersistentFlags().String("server", "localhost:80", "<host>:<port> of instance to connect to.\n")
	cmd.PersistentFlags().StringP("log", "l", cfgdir.DefaultStdLogPath, "log location for developer logs.\n")
	cmd.PersistentFlags().String("loglevel", "DEBUG", "log level for developer logs (-l).\n"+
		"Possible values: 'OFF', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'CRITICAL', 'FATAL'.\n")
	cmd.PersistentFlags().Bool("insecure", false, "do not use HTTPS and do not enforce certs.")
	cmd.PersistentFlags().String("profile", "", "spins up the native CPU profiler to log samples (in pprof format) into the given path")
	cmd.PersistentFlags().MarkHidden("profile")
}

// WalkResult is the outcome of a Walk() call.
// It represents the properties found from parsing a user input string.
// Currently, Builtin and EndCmd are mutually exclusive; if one is set then you can assume the other is not.
// It is conceivable that future builtins will be context-aware of the cmd they are to run on, but that is currently not the case (as Help has special handling).
// Relatedly, Builtin should not contain "Help" unless HelpMode is also set.
// This is because HelpMode represents that the caller should invoke help;
// if Builtin contains Help, then it is because the user activated HelpMode on the "help" builtin.
type WalkResult struct {
	EndCmd          *cobra.Command // the last nav or action seen.
	RemainingTokens []string       // all tokens remaining after endCmd
	Builtin         string         // the builtin to trigger; it will only contain "help" if HelpMode is also set (requesting help about help).
	HelpMode        bool           // display help for the endCmd or builtin, rather than invoking it
}

// Walk traverses the given user input and returns how to handle it (and whether or not it is erroneous).
// It assumes input has the form ["help"] <command path> [flags] and will error if this form is not met.
// Parsing stops when a flag is found, an action is found, no tokens remain, or an error occurred.
// If an error is returned, WalkResult will contain the state of Walk when the error was encountered.
func Walk(pwd *cobra.Command, input string, builtinActions []string) (WalkResult, error) {
	if pwd == nil {
		return WalkResult{}, errors.New("pwd cannot be nil")
	}

	// setup
	var wg sync.WaitGroup

	// transmute builtins to a hashset
	wg.Add(1)
	var biSet map[string]bool
	go func() {
		defer wg.Done()
		biSet = make(map[string]bool, len(builtinActions))
		for _, biAct := range builtinActions {
			biSet[biAct] = true
		}
	}()

	// split input
	wg.Add(1)
	var (
		tokens []string
		err    error
	)
	go func() {
		defer wg.Done()
		tokens, err = shlex.Split(strings.TrimSpace(input))
	}()
	wg.Wait()

	if err != nil {
		return WalkResult{}, err
	} else if len(tokens) < 1 {
		return WalkResult{
			EndCmd: pwd,
		}, nil
	}

	// check for "help" mode or help flags on pwd
	var helpMode bool
	switch tokens[0] {
	case "-h", "--help":
		return WalkResult{EndCmd: pwd, HelpMode: true}, nil
	case "help":
		helpMode = true
		// check for "help help"
		if len(tokens[1:]) > 0 && tokens[1] == "help" {
			return WalkResult{
				EndCmd:          nil,
				RemainingTokens: tokens[2:],
				Builtin:         "help",
				HelpMode:        true,
			}, nil
		}
		tokens = tokens[1:]
	}

	endCmd, excessTokens, builtin, unknownToken := findEndCommand(pwd, slices.Clip(tokens), biSet)
	// transform the results into a WalkResult
	wr := WalkResult{
		EndCmd:          endCmd,
		RemainingTokens: excessTokens,
		Builtin:         builtin,
	}
	// check for errors
	if unknownToken != "" {
		return wr, errors.New(unknownToken + " is not a valid child command of " + stylesheet.ColorCommandName(endCmd) + " or builtin")
	} else if builtin == "help" {
		// we explicitly check for help prior to findEndCommand.
		// if it was found again, then this must have been bad input
		return wr, fmt.Errorf("help must be of the form %v. See %v for more help",
			stylesheet.Cur.ExampleText.Render("help "+ft.MutuallyExclusive([]string{"command path"})),
			stylesheet.Cur.ExampleText.Render("help help"))
	}
	// look ahead for -h/--help
	if slices.ContainsFunc(excessTokens, func(tkn string) bool { return tkn == "-h" || tkn == "--help" }) {
		helpMode = true
		// clip out the help flags so remaining tokens is consistent
		wr.RemainingTokens = slices.DeleteFunc(excessTokens, func(tkn string) bool { return tkn == "-h" || tkn == "--help" })
	}
	wr.HelpMode = helpMode
	return wr, nil
}

// findEndCommand is the underlying, recursive driver for Walk.
// It traverses tokens to identify what nav, action, or builtin the user was attempting to invoke.
// Stops on the first flag, action, or builtin it finds.
//
// pwd is our current position.
// remainingTokens is the shlex'd tokens that have not yet been processed.
// builtins is a hashset of builtin action names.
//
// end is the last valid cobra command found. It will always be populated.
// excessTokens is extra tokens remaining post-traversal.
// builtinInvoked is the name of the builtin to be invoked. Will be empty if the user did not invoke a builtin.
// unknownToken is the non-flag token that stopped processing. Flags stop processing without returning unknown token.
func findEndCommand(pwd *cobra.Command, remainingTokens []string, builtins map[string]bool) (end *cobra.Command, excessTokens []string, builtinInvoked string, unknownToken string) {
	if len(remainingTokens) == 0 { // nothing left to parse, return current state
		return pwd, nil, "", ""
	}
	// cut the first token
	curTkn, remainingTokens := strings.TrimSpace(remainingTokens[0]), remainingTokens[1:]
	if curTkn == "" { // ignore extra whitespace
		return findEndCommand(pwd, remainingTokens, builtins)
	} else if curTkn[0] == '-' { // found a flag or flag-like token
		return pwd, nil, "", ""
	}
	// special tokens have the highest priority
	switch curTkn {
	case "..": // up
		return findEndCommand(up(pwd), remainingTokens, builtins)
	case "~", "/": // root
		return findEndCommand(pwd.Root(), remainingTokens, builtins)
	}
	// child commands have next highest priority
	for _, child := range pwd.Commands() {
		if child.Name() == curTkn || child.HasAlias(curTkn) {
			if action.Is(child) {
				return child, remainingTokens, "", ""
			}
			// keep traversing navs
			return findEndCommand(child, remainingTokens, builtins)
		}
	}
	if _, found := builtins[curTkn]; found {
		return pwd, remainingTokens, curTkn, ""
	}

	return pwd, remainingTokens, "", curTkn
}

// Return the parent directory to the given command
func up(dir *cobra.Command) *cobra.Command {
	if dir.Parent() == nil { // if we are at root, do nothing
		return dir
	}
	// otherwise, step upward
	return dir.Parent()
}
