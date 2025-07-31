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
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/shlex"
	"github.com/gravwell/gravwell/v4/gwcli/action"
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
	Builtin         string         // the non-help builtin to trigger
	HelpMode        bool           // help was request for the named builtin or EndCmd (whichever is set)
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
		tokens, err = shlex.Split(input)
	}()
	wg.Wait()

	if err != nil {
		return WalkResult{}, err
	}

	return innerWalk(pwd, slices.Clip(tokens), biSet, false)
}

// innerWalk is the underlying, recursive driver for Walk.
// pwd is our current position.
// remainingTokens is the shlex'd tokens that have not yet been processed.
// builtins is a hashset of builtin action names.
// helpMode is set to true if the help builtin is found.
func innerWalk(pwd *cobra.Command, remainingTokens []string, builtins map[string]bool, helpMode bool) (WalkResult, error) {
	if len(remainingTokens) == 0 { // nothing left to parse, return current state
		return WalkResult{
			EndCmd:   pwd,
			HelpMode: helpMode,
		}, nil
	}
	// cut the first token
	curTkn, remainingTokens := strings.TrimSpace(remainingTokens[0]), remainingTokens[1:]
	if curTkn == "" { // ignore extra whitespace
		return innerWalk(pwd, remainingTokens, builtins, helpMode)
	}
	// special tokens have the highest priority
	switch curTkn {
	case "..": // up
		return innerWalk(up(pwd), remainingTokens, builtins, helpMode)
	case "~", "/": // root
		return innerWalk(pwd.Root(), remainingTokens, builtins, helpMode)
	}
	// child commands have next highest priority
	var nextCmd *cobra.Command
	for _, child := range pwd.Commands() {
		if child.Name() == curTkn || child.HasAlias(curTkn) {
			nextCmd = child
			break
		}
	}
	if nextCmd != nil { // found a matching child
		if action.Is(nextCmd) {
			wr := WalkResult{
				EndCmd:          nextCmd,
				RemainingTokens: remainingTokens,
				// if we are not already in help mode, check remaining tokens for -h/--help
				HelpMode: helpMode || slices.ContainsFunc(remainingTokens, func(item string) bool { return item == "-h" || item == "--help" }),
			}
			return wr, nil
		}
		// navs keep walking so long as the next token is not -h/--help
		if remainingTokens[0] == "-h" || remainingTokens[0] == "--help" {
			return WalkResult{
				EndCmd:          nextCmd,
				RemainingTokens: remainingTokens,
				Builtin:         "",
				HelpMode:        true,
			}, nil
		}
		return innerWalk(nextCmd, remainingTokens, builtins, helpMode)
	}
	// finally, check builtins
	if curTkn == "help" { // special handling for "help"
		// TODO help as the first token should be checked by Walk()
		// if we find it again, this is bad input
		// return "help keyword found multiple times"
	}
	/*if nextCmd == nil {
		// NOTE(rlandau): remaining tokens does not (currently) include the erroneous token
		return WalkResult{EndCmd: pwd, RemainingTokens: remainingTokens}, fmt.Errorf("%s is not a known child of %v", curTkn, pwd.Name())
	}*/

	// handle builtin commands
	if curTkn == "help" { // help has special handling
		// if helpMode is already set, then the user is requesting help about help
		if helpMode {
			return WalkResult{
				EndCmd:   nil,
				Builtin:  "help",
				HelpMode: true,
			}, nil
		}
		// check the next token for -h/--help
		//if len(remainingTokens) > 0 && (remainingTokens[0] == "")

	} else if _, found := builtins[curTkn]; found {
		return WalkResult{
			EndCmd:          pwd,
			RemainingTokens: remainingTokens,
			Builtin:         curTkn,
		}, nil
	}

}

// Return the parent directory to the given command
func up(dir *cobra.Command) *cobra.Command {
	if dir.Parent() == nil { // if we are at root, do nothing
		return dir
	}
	// otherwise, step upward
	//clilog.Writer.Debugf("Up: %v -> %v", dir.Name(), dir.Parent().Name())
	return dir.Parent()
}
