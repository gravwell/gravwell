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

type WalkResult struct {
	EndCmd *cobra.Command // the last nav or action seen
	//activate        bool           // should endCmd be called (moved to, in the case of a nav)?
	RemainingTokens []string // all tokens remaining after endCmd
	Builtin         string   // the non-help builtin to trigger
}

// Walk recursively traverses input, searching for a command or builtin to match on.
// If an error is returned, do not rely on any information in WalkResult.
func Walk(pwd *cobra.Command, input string, builtinActions []string) (WalkResult, error) {
	if pwd == nil {
		return WalkResult{}, errors.New("pwd cannot be nil")
	}
	// transmute builtins to a hashset
	var biSet = make(map[string]bool, len(builtinActions))
	for _, biAct := range builtinActions {
		biSet[biAct] = true
	}
	// split input
	tokens, err := shlex.Split(input)
	if err != nil {
		return WalkResult{}, err
	}
	return innerWalk(pwd, slices.Clip(tokens), biSet)
}

// innerWalk is the underlying, recursive driver for Walk.
// pwd is our current position.
// remainingTokens is the shlex'd tokens that have not yet been processed.
// builtins is a hashset of builtin action names.
// curRes is the current, in-progress WalkResult.
func innerWalk(pwd *cobra.Command, remainingTokens []string, builtins map[string]bool) (WalkResult, error) {
	if len(remainingTokens) == 0 { // nothing left to parse
		return WalkResult{
			EndCmd: pwd,
		}, nil
	}
	// cut the first token
	curTkn, remainingTokens := strings.TrimSpace(remainingTokens[0]), remainingTokens[1:]
	if curTkn == "" { // no token, keep walking
		return innerWalk(pwd, remainingTokens, builtins)
	}
	// check for a built-in command
	if _, found := builtins[curTkn]; found {
		return WalkResult{
			EndCmd:          pwd,
			RemainingTokens: remainingTokens,
			Builtin:         curTkn,
		}, nil
	}

	// check for special tokens
	switch curTkn {
	case "..": // upward
		return innerWalk(up(pwd), remainingTokens, builtins)
	case "~", "/": // root
		return innerWalk(pwd.Root(), remainingTokens, builtins)
	case "-h", "--help": // the only flags we handle are the help flags
		return WalkResult{
			EndCmd:          pwd,
			RemainingTokens: remainingTokens,
			Builtin:         "help",
		}, nil
	}

	// check for a child command
	var nextCmd *cobra.Command
	for _, chld := range pwd.Commands() {
		if chld.Name() == curTkn || chld.HasAlias(curTkn) {
			//clilog.Writer.Debugf("child match on %s", curTkn)
			nextCmd = chld
			break
		}
	}
	if nextCmd == nil {
		// NOTE(rlandau): remaining tokens does not (currently) include the erroneous token
		return WalkResult{EndCmd: pwd, RemainingTokens: remainingTokens}, fmt.Errorf("%s is not a known child of %v", curTkn, pwd.Name())
	}
	// split on nav or action
	if action.Is(nextCmd) {
		// look ahead for help flags
		wr := WalkResult{
			EndCmd:          nextCmd,
			RemainingTokens: remainingTokens,
		}
		if slices.ContainsFunc(remainingTokens, func(item string) bool { return item == "-h" || item == "--help" }) {
			wr.Builtin = "help"
		}
		return wr, nil
	}
	// found a nav, keep walking
	return innerWalk(nextCmd, remainingTokens, builtins)

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
