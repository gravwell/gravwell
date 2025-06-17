/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package query

import (
	"fmt"
	"time"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/querysupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
)

/*
Collection of subroutines and pre-formatted strings used by multiple of the three modes (script, interactive, and mother) to ensure consistency.
*/

//#region strings

// The given search associated to SID was submitted in the background and our job is done.
func querySubmissionSuccess(sid string, background bool) string {
	var fgbg = "foreground"
	if background {
		fgbg = "background"
	}
	return fmt.Sprintf("Successfully submitted %s query (ID: %s)", fgbg, sid)
}

//#endregion strings

//#region helper functions

// Generates a scheduling request from the given flags, cmd, and query and attempts to schedule it.
// Assumes the query has already been validated.
func scheduleQuery(flags *querysupport.QueryFlags, validatedQry string) (ssid int32, warnings []string, invalid string, err error) {
	// warn about ignored flags
	if clilog.Active(clilog.WARN) { // only warn if WARN level is enabled
		warnings = make([]string, 0)
		if flags.OutPath != "" {
			warnings = append(warnings, ft.WarnFlagIgnore(ft.Name.Output, ft.Name.Frequency))
		}
		if flags.Background {
			warnings = append(warnings, ft.WarnFlagIgnore("background", ft.Name.Frequency))
		}
		if flags.Append {
			warnings = append(warnings, ft.WarnFlagIgnore(ft.Name.Append, ft.Name.Frequency))
		}
		if flags.JSON {
			warnings = append(warnings, ft.WarnFlagIgnore(ft.Name.JSON, ft.Name.Frequency))
		}
		if flags.CSV {
			warnings = append(warnings, ft.WarnFlagIgnore(ft.Name.CSV, ft.Name.Frequency))
		}
	}

	// if a name was not given, populate a default name
	if flags.Schedule.Name == "" {
		flags.Schedule.Name = "cli_" + time.Now().Format(uniques.SearchTimeFormat)
	}
	// if a description was not given, populate a default description
	if flags.Schedule.Desc == "" {
		flags.Schedule.Desc = "generated in gwcli @" + time.Now().Format(uniques.SearchTimeFormat)
	}

	ssid, invalid, err = connection.CreateScheduledSearch(
		flags.Schedule.Name, flags.Schedule.Desc,
		flags.Schedule.CronFreq, validatedQry,
		flags.Duration,
	)
	if invalid != "" { // bad parameters
		return -1, warnings, invalid, err
	} else if err != nil {
		return -1, warnings, "", err
	}
	return ssid, warnings, "", nil
}

// Returns whether or not the given query if valid (or if an non-parse error occurred while asking the backend to eval the query).
func testQryValidity(qry string) (valid bool, err error) {
	if qry == "" {
		return false, nil
	}

	err = connection.Client.ParseSearch(qry)
	// check if this is a parse error or something else
	if err != nil {
		clilog.Writer.Infof("failed to parse search %v: %v", qry, err)
		return false, err
	}
	return true, nil
}

// Returns any and all warnings related to other flags being ignored due to the existence of --background.
// Does not check the schedule flags; assumes scheduling was handled first.
// Only returns data if the clilog is set to print at WARNING level.
func warnBackgroundFlagConflicts(flags querysupport.QueryFlags) (warnings []string) {
	if clilog.Active(clilog.WARN) { // only warn if WARN level is enabled
		warnings = make([]string, 0)
		if flags.OutPath != "" {
			warnings = append(warnings, ft.WarnFlagIgnore(ft.Name.Output, "background")+"\n")
		}
		if flags.Append {
			warnings = append(warnings, ft.WarnFlagIgnore(ft.Name.Append, "background")+"\n")
		}
		if flags.JSON {
			warnings = append(warnings, ft.WarnFlagIgnore(ft.Name.JSON, "background")+"\n")
		}
		if flags.CSV {
			warnings = append(warnings, ft.WarnFlagIgnore(ft.Name.CSV, "background")+"\n")
		}
	}

	return warnings
}

//#endregion helper functions
