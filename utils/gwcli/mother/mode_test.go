/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mother

import "testing"

func Test_mode_String(t *testing.T) {
	tests := []struct {
		name string
		m    mode
		want string
	}{
		{"handoff", handoff, "handoff"},
		{"prompting", prompting, "prompting"},
		{"quitting", quitting, "quitting"},
		{"unknown", 5, "unknown (5)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.String(); got != tt.want {
				t.Errorf("mode.String() = %v, want %v", got, tt.want)
			}
		})
	}
}
