/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package uniques

import (
	"testing"

	"github.com/Pallinder/go-randomdata"
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
