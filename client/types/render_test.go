/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/utils/jsoncompat"
	"github.com/stretchr/testify/require"
)

func TestResultsRequestStatsOverDecode(t *testing.T) {
	width := types.ResultsRequestStatsOver{OverWidth: &types.ResultsRequestStatsOverWidth{Width: 30}}
	count := types.ResultsRequestStatsOver{OverCount: &types.ResultsRequestStatsOverCount{Count: 12}}

	tests := []struct {
		name    string
		body    string
		want    types.ResultsRequestStatsOver
		wantErr error
	}{
		{name: "Width", body: `{"Width": 30}`, want: width},
		{name: "Count", body: `{"Count": 12}`, want: count},
		{name: "lowercase width", body: `{"width": 30}`, want: width},
		{name: "lowercase count", body: `{"count": 12}`, want: count},
		{name: "Width and Count", body: `{"Width": 30, "Count": 12}`, wantErr: types.ErrResultsRequestStatsOverInvalid},
		{name: "empty object", body: `{}`, wantErr: types.ErrResultsRequestStatsOverEmpty},
		{name: "null", body: `null`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got types.ResultsRequestStatsOver
			err := json.Unmarshal([]byte(tt.body), &got)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// The request body documented for POST /api/searches/{id}/results, decoded
// as the webserver decodes it.
func TestResultsRequestDecodeStatsOver(t *testing.T) {
	body := `{"SID": "s", "Stats": {"Operations": [{"Operation": "count"}], "Over": {"Width": 30}}}`

	var got types.ResultsRequest
	require.NoError(t, json.UnmarshalRead(strings.NewReader(body), &got, jsoncompat.Opts))
	over := got.Stats.Value().Over.Value()
	require.Equal(t, &types.ResultsRequestStatsOverWidth{Width: 30}, over.OverWidth)
	require.Nil(t, over.OverCount)
}

// A left-out Stats or Over is unset. Null is rejected, and so is an Over
// with neither Width nor Count.
func TestResultsRequestDecodeStatsPresence(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantStats bool
		wantOver  bool
		wantErr   error
	}{
		{name: "no Stats", body: `{"SID": "s"}`},
		{name: "empty Stats", body: `{"SID": "s", "Stats": {}}`, wantStats: true},
		{name: "empty Operations", body: `{"SID": "s", "Stats": {"Operations": []}}`, wantStats: true},
		{name: "no Over", body: `{"SID": "s", "Stats": {"Operations": [{"Operation": "count"}]}}`, wantStats: true},
		{name: "Over", body: `{"SID": "s", "Stats": {"Operations": [{"Operation": "count"}], "Over": {"Width": 30}}}`, wantStats: true, wantOver: true},
		{name: "null Stats", body: `{"SID": "s", "Stats": null}`, wantErr: types.ErrNoNullOptional},
		{name: "null Over", body: `{"SID": "s", "Stats": {"Operations": [{"Operation": "count"}], "Over": null}}`, wantErr: types.ErrNoNullOptional},
		{name: "empty Over", body: `{"SID": "s", "Stats": {"Operations": [{"Operation": "count"}], "Over": {}}}`, wantErr: types.ErrResultsRequestStatsOverEmpty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got types.ResultsRequest
			err := json.UnmarshalRead(strings.NewReader(tt.body), &got, jsoncompat.Opts)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantStats, got.Stats.IsSet())
			require.Equal(t, tt.wantOver, got.Stats.Value().Over.IsSet())
		})
	}
}

// A request with no Over encodes without the key, so it decodes as unset.
func TestResultsRequestRoundTripNoOver(t *testing.T) {
	src := types.ResultsRequest{
		SID: "s",
		Stats: types.NewOptional(types.ResultsRequestStats{
			Operations: []types.StatsOperation{{Operation: "count"}},
		}),
	}
	enc, err := json.Marshal(src, jsoncompat.Opts)
	require.NoError(t, err)

	var got types.ResultsRequest
	require.NoError(t, json.Unmarshal(enc, &got, jsoncompat.Opts), "decoding %s", enc)
	require.True(t, got.Stats.IsSet())
	require.False(t, got.Stats.Value().Over.IsSet())
}

func TestResultsRequestStatsOverRoundTrip(t *testing.T) {
	for _, src := range []types.ResultsRequestStatsOver{
		{OverWidth: &types.ResultsRequestStatsOverWidth{Width: 30}},
		{OverCount: &types.ResultsRequestStatsOverCount{Count: 12}},
	} {
		enc, err := json.Marshal(&src)
		require.NoError(t, err)

		var got types.ResultsRequestStatsOver
		require.NoError(t, json.Unmarshal(enc, &got), "decoding %s", enc)
		require.Equal(t, src, got)
	}
}
