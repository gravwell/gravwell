/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package systemshealth

import (
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/annotations"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/utils/weave"
	"github.com/spf13/pflag"
)

// searchAgentRow is a flattened, per-agent view of types.SearchAgentInfo, suitable for scaffoldlist.
// Search_Agent_Auth is intentionally omitted here, since it is a bearer secret and should NEVER be surfaced.
type searchAgentRow struct {
	UUID                          string
	LastCheckin                   time.Time
	ActiveJobs                    []string
	WebserverAddress              []string
	DisableNetworkScriptFunctions bool
	DisableSelfIngest             bool
}

// searchAgentInfosToRows flattens the per-agent data returned by GetSearchAgentStatus into a
// slice of searchAgentRow, suitable for scaffoldlist rendering.
func searchAgentInfosToRows(agents []types.SearchAgentInfo) []searchAgentRow {
	rows := make([]searchAgentRow, len(agents))

	for idx, a := range agents {
		rows[idx] = searchAgentRow{
			UUID:                          a.Cfg.Searchagent_UUID,
			LastCheckin:                   a.LastCheckin,
			ActiveJobs:                    a.ActiveJobs,
			WebserverAddress:              a.Cfg.Webserver_Address,
			DisableNetworkScriptFunctions: a.Cfg.Disable_Network_Script_Functions,
			DisableSelfIngest:             a.Cfg.Disable_Self_Ingest,
		}
	}

	return rows
}

// newSearchAgentAction returns an action for reviewing the state of search agents that have
// checked in with this Gravwell instance.
func newSearchAgentAction() action.Pair {
	const (
		use   string = "searchagent"
		short string = "review search agent state"
		long  string = `Review the state of search agents that have checked in with this Gravwell instance.
The aggregate check-in warning is only shown in the default (pretty) view.
Use --json/--csv/--table to script against per-agent data.`
	)

	return scaffoldlist.NewListAction(short, long, searchAgentRow{}, func(addtlFlags *pflag.FlagSet, params scaffoldlist.DataParameters) ([]searchAgentRow, error) {
		status, err := connection.Client.GetSearchAgentStatus()
		if err != nil {
			return nil, err
		}
		return searchAgentInfosToRows(status.SchedulerStatus.SearchAgents), nil
	}, nil, scaffoldlist.Options{
		CommonOptions: scaffold.CommonOptions{
			Use: use,
			Requirements: annotations.Requirements{
				IPermissions: []types.Capability{types.ScheduleRead},
				XPermissions: []types.Capability{types.ScheduleRead},
			},
		},
		DefaultColumns: []string{"UUID", "LastCheckin", "ActiveJobs"},
		EmptyMessage:   "no search agents have checked in",
		Pretty: func(_ *pflag.FlagSet, dqColumns []string, dqToAlias map[string]string, _ scaffoldlist.DataParameters) (string, error) {
			status, err := connection.Client.GetSearchAgentStatus()
			if err != nil {
				return "", err
			}

			var sb strings.Builder
			if status.Warning {
				sb.WriteString(stylesheet.Cur.ErrorText.Render("Warning: no search agent has checked in recently; scheduled automations may not be running."))
				sb.WriteString("\n")
			}
			sb.WriteString(stylesheet.Cur.Field("Last Checkin", 17))
			sb.WriteString(status.LastCheckin.String())
			sb.WriteString("\n\n")

			rows := searchAgentInfosToRows(status.SchedulerStatus.SearchAgents)
			if len(rows) == 0 {
				sb.WriteString("no search agents have checked in")
				return sb.String(), nil
			}

			sb.WriteString(weave.ToTable(rows, dqColumns, weave.TableOptions{
				Base:            stylesheet.Table,
				Aliases:         dqToAlias,
				HeaderWrapWidth: stylesheet.TableColumnContentWidth(),
			}))

			return sb.String(), nil
		},
		QueryOptionsFlags: scaffold.QOOmit{Everything: true},
	})
}
