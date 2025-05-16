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
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
)

/*
Collection of subroutines and pre-formatted strings used by multiple of the three modes (script, interactive, and mother) to ensure consistency.
*/

//#region strings

// The given search associated to SID was submitted in the background and our job is done.
func querySubmissionSuccess(sid string, background bool) string {
	var fgbg string = "foreground"
	if background {
		fgbg = "background"
	}
	return fmt.Sprintf("Successfully submitted %s query (ID: %s)", fgbg, sid)
}

//#endregion strings

//#region helper functions

// Generates a scheduling request from the given flags, cmd, and query and attempts to schedule it.
// Assumes the query has already been validated.
func scheduleQuery(flags *queryflags, validatedQry string) (ssid int32, warnings []string, invalid string, err error) {
	// warn about ignored flags
	if clilog.Active(clilog.WARN) { // only warn if WARN level is enabled
		warnings = make([]string, 0)
		if flags.outfn != "" {
			warnings = append(warnings, uniques.WarnFlagIgnore(ft.Name.Output, ft.Name.Frequency))
		}
		if flags.background {
			warnings = append(warnings, uniques.WarnFlagIgnore("background", ft.Name.Frequency))
		}
		if flags.append {
			warnings = append(warnings, uniques.WarnFlagIgnore(ft.Name.Append, ft.Name.Frequency))
		}
		if flags.json {
			warnings = append(warnings, uniques.WarnFlagIgnore(ft.Name.JSON, ft.Name.Frequency))
		}
		if flags.csv {
			warnings = append(warnings, uniques.WarnFlagIgnore(ft.Name.CSV, ft.Name.Frequency))
		}
	}

	// if a name was not given, populate a default name
	if flags.schedule.name == "" {
		flags.schedule.name = "cli_" + time.Now().Format(uniques.SearchTimeFormat)
	}
	// if a description was not given, populate a default description
	if flags.schedule.desc == "" {
		flags.schedule.desc = "generated in gwcli @" + time.Now().Format(uniques.SearchTimeFormat)
	}

	ssid, invalid, err = connection.CreateScheduledSearch(
		flags.schedule.name, flags.schedule.desc,
		flags.schedule.cronfreq, validatedQry,
		flags.duration,
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
func warnBackgroundFlagConflicts(flags queryflags) (warnings []string) {
	if clilog.Active(clilog.WARN) { // only warn if WARN level is enabled
		warnings = make([]string, 0)
		if flags.outfn != "" {
			warnings = append(warnings, uniques.WarnFlagIgnore(ft.Name.Output, "background")+"\n")
		}
		if flags.append {
			warnings = append(warnings, uniques.WarnFlagIgnore(ft.Name.Append, "background")+"\n")
		}
		if flags.json {
			warnings = append(warnings, uniques.WarnFlagIgnore(ft.Name.JSON, "background")+"\n")
		}
		if flags.csv {
			warnings = append(warnings, uniques.WarnFlagIgnore(ft.Name.CSV, "background")+"\n")
		}
	}

	return warnings
}

//#endregion helper functions
