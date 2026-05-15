/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package validate provides functions for uniformly validating input.
package validate

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Numeric returns an error if s is not composed solely of digits.
func Numeric(s string) (err error) {
	s = strings.TrimSpace(s)
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return fmt.Errorf("'%q' is not a valid numeral", r)
		}
	}
	return nil
}

// PortNumber attempts to convert s into a valid port number between [1-65535]
func PortNumber(s string) (port uint16, err error) {
	if err := Numeric(s); err != nil {
		return 0, err
	}
	p64, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	if p64 == 0 || p64 > math.MaxUint16 {
		return 0, fmt.Errorf("%s is not a valid number between [1,%d]", s, math.MaxUint16)
	}
	return uint16(p64), nil
}

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
