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
	"testing"
)

func TestSlicesUnorderedEqual(t *testing.T) {
	type args struct {
		a []string
		b []string
	}
	tests := []struct {
		name      string
		args      args
		wantEqual bool
	}{
		{"both nil", args{nil, nil}, true},
		{"unequal, same length", args{[]string{"Underdark"}, []string{"Darklands"}}, false},
		{"unequal, different lengths", args{[]string{"Underdark"}, []string{"Darklands", "Rappan Athuk"}}, false},
		{"unequal, different lengths, same starter items", args{[]string{"Darklands", "Rappan Athuk", "Vaults"}, []string{"Darklands", "Rappan Athuk"}}, false},
		{"equal, same order", args{[]string{"Nar-Voth", "Sekamina", "Orv"}, []string{"Nar-Voth", "Sekamina", "Orv"}}, true},
		{"equal, different order", args{[]string{"Nar-Voth", "Orv", "Sekamina"}, []string{"Nar-Voth", "Sekamina", "Orv"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SlicesUnorderedEqual(tt.args.a, tt.args.b); got != tt.wantEqual {
				t.Errorf("SlicesUnorderedEqual() = %v, want %v", got, tt.wantEqual)
			}
		})
	}
}
