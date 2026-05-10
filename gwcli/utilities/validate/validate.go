/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package Validate provides functions for uniformly validating input.
package validate

import (
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
