/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package querysupport

import (
	"strings"
	"time"

	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"

	"github.com/spf13/pflag"
)

// A Schedule carries just enough information to schedule a query.
type Schedule struct {
	Name     string
	Desc     string
	CronFreq string // run frequency in cron format
}

// QueryFlags holds the parsed values of all flags that are useful for querying actions.
// Actions do not have to implement handling for each flag.
//
// Should be constructed via TransmogrifyFlags().
type QueryFlags struct {
	Duration   time.Duration
	Script     bool
	JSON       bool
	CSV        bool
	OutPath    string
	Append     bool
	Schedule   Schedule
	Background bool
	//referenceID string
}

// TransmogrifyFlags takes a *parsed* flagset and returns a structured, typed, and (in the case of strings) trimmed representation of the flags therein.
//
// While it will coerce data to an appropriate type, transmogrify will *not* check for the state of required or dependent flags.
//
// NOTE(rlandau): fields in QueryFlags that are not defined in the flagset (aka not handled in the action) will be silently zeroed, swallowing errors.
func TransmogrifyFlags(fs *pflag.FlagSet) QueryFlags {
	var qf QueryFlags

	qf.Duration, _ = fs.GetDuration("duration")
	qf.Script, _ = fs.GetBool(ft.Name.Script)
	qf.JSON, _ = fs.GetBool(ft.Name.JSON)
	qf.CSV, _ = fs.GetBool(ft.Name.CSV)
	qf.Append, _ = fs.GetBool(ft.Name.Append)
	qf.Background, _ = fs.GetBool("background")

	qf.OutPath, _ = fs.GetString(ft.Name.Output)
	qf.OutPath = strings.TrimSpace(qf.OutPath)

	qf.Schedule.CronFreq, _ = fs.GetString(ft.Name.Frequency)
	qf.Schedule.CronFreq = strings.TrimSpace(qf.Schedule.CronFreq)

	qf.Schedule.Name, _ = fs.GetString(ft.Name.Name)
	qf.Schedule.Name = strings.TrimSpace(qf.Schedule.Name)

	qf.Schedule.Desc, _ = fs.GetString(ft.Name.Desc)
	qf.Schedule.Desc = strings.TrimSpace(qf.Schedule.Desc)

	return qf
}
