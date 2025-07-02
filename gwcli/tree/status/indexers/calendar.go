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
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
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
				c.Args = cobra.MaximumNArgs(1) // indexer uuid may be specified
			},
		})
}

func calendarFlags() pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.String("start", "", "start time for calendar stats.\n"+
		"May be given in RFC1123Z (Mon, 02 Jan 2006 15:04:05 -0700) or DateTime (2006-01-02 15:04:05).\n"+
		"If unset, defaults to now.")
	fs.String("end", "", "end time for calendar stats.\n"+
		"May be given in RFC1123Z (Mon, 02 Jan 2006 15:04:05 -0700) or DateTime (2006-01-02 15:04:05).\n"+
		"If unset, defaults to now.")
	fs.StringSlice("wells", nil, "specify the wells to fetch data for.\n"+
		"If unspecified, all wells will be selected.")
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
	var indexer = uuid.Nil
	if fs.NArg() > 0 {
		idxr, err := uuid.Parse(fs.Arg(0))
		if err != nil {
			return nil, err
		}
		indexer = idxr
	}

	clilog.Writer.Infof("fetching calendar entries for indexer %v", indexer)
	clilog.Writer.Debugf("start=%v|end=%v|wells=%v", start, end, wells)

	// if an indexer was specified, get stats for that specific indexer
	if indexer != uuid.Nil {
		return connection.Client.GetIndexerCalendarStats(indexer, start, end, nil)
	}

	return connection.Client.GetCalendarStats(start, end, nil)
}

// fetchTime attempts to parse the given flag as a time.Time.
// If the flag was unset, fetchTime will return time.Time{} and nil.
func fetchTime(fs *pflag.FlagSet, flagName string) (time.Time, error) {
	// check for and parse start and end flags
	s, err := fs.GetString(flagName)
	if err != nil {
		return time.Time{}, uniques.ErrGetFlag(useCalendar, err)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now(), nil
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
