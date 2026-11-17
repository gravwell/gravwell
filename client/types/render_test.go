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
		{name: "empty object", body: `{}`},
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
	require.Equal(t, &types.ResultsRequestStatsOverWidth{Width: 30}, got.Stats.Over.OverWidth)
	require.Nil(t, got.Stats.Over.OverCount)
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
