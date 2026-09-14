/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package systemshealth

import (
	"fmt"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/stretchr/testify/assert"
)

func TestSearchAgentInfosToRows(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-5 * time.Minute)

	tests := []struct {
		name   string
		agents []types.SearchAgentInfo
		want   []searchAgentRow
	}{
		{
			name:   "nil input",
			agents: nil,
			want:   []searchAgentRow{},
		},
		{
			name:   "empty input",
			agents: []types.SearchAgentInfo{},
			want:   []searchAgentRow{},
		},
		{
			name: "single agent",
			agents: []types.SearchAgentInfo{
				{
					Cfg: types.SearchAgentConfig{
						Searchagent_UUID:                 "57313ba4-e93b-41fc-8332-be7e78d3fe5d",
						Webserver_Address:                []string{"gravwell.localhost:80"},
						Disable_Network_Script_Functions: true,
						Disable_Self_Ingest:              false,
						Search_Agent_Auth:                "super-secret-token",
					},
					LastCheckin: now,
					ActiveJobs:  []string{"job-1"},
				},
			},
			want: []searchAgentRow{
				{
					UUID:                          "57313ba4-e93b-41fc-8332-be7e78d3fe5d",
					LastCheckin:                   now,
					ActiveJobs:                    []string{"job-1"},
					WebserverAddress:              []string{"gravwell.localhost:80"},
					DisableNetworkScriptFunctions: true,
					DisableSelfIngest:             false,
				},
			},
		},
		{
			name: "multiple agents, order preserved",
			agents: []types.SearchAgentInfo{
				{
					Cfg:         types.SearchAgentConfig{Searchagent_UUID: "agent-a"},
					LastCheckin: earlier,
					ActiveJobs:  []string{"job-a1", "job-a2"},
				},
				{
					Cfg:         types.SearchAgentConfig{Searchagent_UUID: "agent-b"},
					LastCheckin: now,
					ActiveJobs:  nil,
				},
			},
			want: []searchAgentRow{
				{UUID: "agent-a", LastCheckin: earlier, ActiveJobs: []string{"job-a1", "job-a2"}},
				{UUID: "agent-b", LastCheckin: now, ActiveJobs: nil},
			},
		},
		{
			name: "agent with empty ActiveJobs",
			agents: []types.SearchAgentInfo{
				{
					Cfg:         types.SearchAgentConfig{Searchagent_UUID: "agent-c"},
					LastCheckin: now,
					ActiveJobs:  []string{},
				},
			},
			want: []searchAgentRow{
				{UUID: "agent-c", LastCheckin: now, ActiveJobs: []string{}},
			},
		},
		{
			name: "agent with multiple ActiveJobs",
			agents: []types.SearchAgentInfo{
				{
					Cfg:         types.SearchAgentConfig{Searchagent_UUID: "agent-d"},
					LastCheckin: now,
					ActiveJobs:  []string{"job-1", "job-2", "job-3"},
				},
			},
			want: []searchAgentRow{
				{UUID: "agent-d", LastCheckin: now, ActiveJobs: []string{"job-1", "job-2", "job-3"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchAgentInfosToRows(tt.agents)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestSearchAgentInfosToRows_NeverLeaksAuthToken guards against a future contributor
// accidentally wiring Search_Agent_Auth (a bearer secret) into searchAgentRow.
func TestSearchAgentInfosToRows_NeverLeaksAuthToken(t *testing.T) {
	const secret = "super-secret-token"

	rows := searchAgentInfosToRows([]types.SearchAgentInfo{
		{
			Cfg: types.SearchAgentConfig{
				Searchagent_UUID:  "agent-e",
				Search_Agent_Auth: secret,
			},
			LastCheckin: time.Now(),
		},
	})

	assert.Len(t, rows, 1)
	assert.NotContains(t, fmt.Sprintf("%+v", rows[0]), secret)
}
