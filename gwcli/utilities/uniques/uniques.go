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
	"os"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/x/term"
)

const (
	// the string format the Gravwell client requires
	SearchTimeFormat = "2006-01-02T15:04:05.999999999Z07:00"
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

// FetchWindowSize queries for available terminal window size.
// Generally useful as an onStart command as Mother does not maintain a set of dimensions.
func FetchWindowSize() tea.Msg {
	w, h, _ := term.GetSize(os.Stdin.Fd())
	return tea.WindowSizeMsg{Width: w, Height: h}
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
