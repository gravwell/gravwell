/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package indexers

// This file implements the indexer calendar action, which returns timestamped entries associated to the named indexer.

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	useCalendar   string = "calendar"
	shortCalendar string = "display calendar entries for an indexer"
	longCalendar  string = "Display day-by-day calendar statistics for a given indexer and its wells."
)

func newCalendarAction() action.Pair {

	var aliases = []string{"entries"}

	return scaffoldlist.NewListAction(shortCalendar, longCalendar, types.CalendarEntry{}, data,
		scaffoldlist.Options{
			Use:        useCalendar,
			Aliases:    aliases,
			AddtlFlags: calendarFlags,
			CmdMods: func(c *cobra.Command) {
				c.MarkFlagRequired("start")
				c.MarkFlagRequired("end")
				//c.MarkFlagsRequiredTogether("start", "end")
				c.Args = cobra.MaximumNArgs(1) // indexer uuid may be specified
			},
		})
}

func calendarFlags() pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.String("start", "", "REQUIRED. Start time for calendar stats.\n"+
		"May be given in RFC1123Z (Mon, 02 Jan 2006 15:04:05 -0700) or DateTime (2006-01-02 15:04:05).\n"+
		"If --start is given, --end must also be specified.")
	fs.String("end", "", "REQUIRED. End time for calendar stats.\n"+
		"May be given in RFC1123Z (Mon, 02 Jan 2006 15:04:05 -0700) or DateTime (2006-01-02 15:04:05).\n"+
		"If --end is given, --start must also be specified.")
	fs.StringSlice("wells", nil, "specify the wells to fetch data for.")
	return fs
}

func data(fs *pflag.FlagSet) ([]types.CalendarEntry, error) {
	start, err := fetchTime(fs, "start")
	if err != nil {
		return nil, err
	}
	end, err := fetchTime(fs, "end")
	if err != nil {
		return nil, err
	}
	wells, err := fs.GetStringSlice("wells")
	if err != nil {
		return nil, uniques.ErrGetFlag(useCalendar, err)
	}
	// if an indexer was specified, get stats for that specific indexer
	if fs.NArg() == 1 {
		indexer, err := uuid.Parse(fs.Arg(0))
		if err != nil {
			return nil, err
		}
		return connection.Client.GetIndexerCalendarStats(indexer, start, end, wells)
	}

	return connection.Client.GetCalendarStats(start, end, wells)
}

// fetchTime attempts to parse the given flag as a time.Time.
// If the flag was unset, fetchTime will return time.Time{} and nil.
func fetchTime(fs *pflag.FlagSet, flagName string) (time.Time, error) {
	// check for and parse start and end flags
	s, err := fs.GetString(flagName)
	if err != nil {
		return time.Time{}, uniques.ErrGetFlag("start", err)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	// attempt to parse time
	if t, err := time.Parse(time.RFC1123Z, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.DateTime, s); err == nil {
		return t, nil
	}

	return time.Time{}, errors.New("--" + flagName + " must be a valid timestamp in RFC1123Z or DateTime format")
}
