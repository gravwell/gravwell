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
	"strings"
	"time"
	"unicode"

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
	// login flags
	cmd.PersistentFlags().StringP("username", "u", "", "login credential. Requires either -p or \""+cfgdir.EnvKeyPassword+"\"."+
		" If your account has MFA enabled, you must use an API token (--api or --eapi) or login interactively.")
	cmd.PersistentFlags().StringP("passfile", "p", "", "the path to a file containing your password")
	cmd.MarkPersistentFlagFilename("passfile")
	ft.API.Register(cmd.PersistentFlags())
	ft.EAPI.Register(cmd.PersistentFlags())
	cmd.MarkFlagsMutuallyExclusive("username", ft.API.Name(), ft.EAPI.Name())

	ft.NoColor.Register(cmd.PersistentFlags())
	cmd.PersistentFlags().String("server", "localhost:80", "<host>:<port> of instance to connect to.\n")
	cmd.PersistentFlags().Bool("insecure", false, "do not use HTTPS and do not enforce certs.")
	cmd.PersistentFlags().String("profile", "", "spins up the native CPU profiler to log samples (in pprof format) into the given path")
	cmd.PersistentFlags().MarkHidden("profile")

	// NOTE: to enable clilog to come online immediately, these flags are never actually handled.
	// Instead, clilog.InitializeFromArgs is used.
	// These definitions are here to act as descriptor text for a user.
	//
	// This is distinction must be made because we cannot parse all flags early as we do not know the full list of acceptable flags until an action has been determined.
	// However, we want the logger to come online early.
	ft.LogPath.Register(cmd.PersistentFlags())
	ft.LogLevel.Register(cmd.PersistentFlags())
}
