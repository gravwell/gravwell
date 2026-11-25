/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"strings"
	"testing"
)

func TestDashboardTimeframeValidate(t *testing.T) {
	denver := "America/Denver"
	empty := ""
	bogus := "Not/AZone"

	tests := []struct {
		name    string
		tf      DashboardTimeframe
		wantErr string
	}{
		{"range", DashboardTimeframe{Kind: "range", Start: "now-1h", End: "now"}, ""},
		{"range in zone", DashboardTimeframe{Kind: "range", Start: "now/d", End: "now", Timezone: &denver}, ""},
		// shape only, the datemath is not evaluated
		{"range with bad datemath", DashboardTimeframe{Kind: "range", Start: "lastHour", End: "now"}, ""},
		{"preview", DashboardTimeframe{Kind: "preview"}, ""},
		{"range missing start", DashboardTimeframe{Kind: "range", End: "now"}, "requires Start and End"},
		{"range missing end", DashboardTimeframe{Kind: "range", Start: "now-1h"}, "requires Start and End"},
		{"empty timezone", DashboardTimeframe{Kind: "range", Start: "now-1h", End: "now", Timezone: &empty}, "may not be empty"},
		{"unknown timezone", DashboardTimeframe{Kind: "range", Start: "now-1h", End: "now", Timezone: &bogus}, "unknown Timezone"},
		{"preview with start", DashboardTimeframe{Kind: "preview", Start: "now-1h"}, "does not accept"},
		{"preview with timezone", DashboardTimeframe{Kind: "preview", Timezone: &denver}, "does not accept"},
		{"missing kind", DashboardTimeframe{}, "missing Kind"},
		{"legacy kind", DashboardTimeframe{Kind: "dates", Start: "now-1h", End: "now"}, "unknown Kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkErr(t, tt.tf.Validate(), tt.wantErr)
		})
	}
}

func TestDashboardSearchLaunchRequestValidate(t *testing.T) {
	good := DashboardTimeframe{Kind: "range", Start: "now-1h", End: "now"}
	bad := DashboardTimeframe{Kind: "range", Start: "now-1h"}

	tests := []struct {
		name    string
		req     DashboardSearchLaunchRequest
		wantErr string
	}{
		{"empty", DashboardSearchLaunchRequest{}, ""},
		{"all set", DashboardSearchLaunchRequest{
			Timeframe:        &good,
			SearchTimeframes: map[string]DashboardTimeframe{"a": good, "b": {Kind: "preview"}},
			Variables:        map[string]string{"%%IP%%": "10.0.0.1", "HOST": ""},
		}, ""},
		{"empty variable name", DashboardSearchLaunchRequest{Variables: map[string]string{"": "x"}}, "empty name"},
		{"bad timeframe", DashboardSearchLaunchRequest{Timeframe: &bad}, "invalid Timeframe: a range timeframe requires Start and End"},
		{"bad entry", DashboardSearchLaunchRequest{SearchTimeframes: map[string]DashboardTimeframe{"a": good, "b": bad}}, `invalid SearchTimeframes["b"]`},
		// the first bad key in sorted order is reported
		{"two bad entries", DashboardSearchLaunchRequest{SearchTimeframes: map[string]DashboardTimeframe{"z": bad, "m": {}}}, `invalid SearchTimeframes["m"]: missing Kind`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkErr(t, tt.req.Validate(), tt.wantErr)
		})
	}
}

// checkErr wants no error when want is empty, else an error containing want.
func checkErr(t *testing.T, err error, want string) {
	t.Helper()

	if want == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want error containing %q, got %v", want, err)
	}
}
