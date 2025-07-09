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
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/pflag"
)

const (
	useCalendar   string = "calendar"
	shortCalendar string = "display calendar entries for an indexer"
	longCalendar  string = "Display day-by-day calendar statistics for a given indexer and its wells."
)

var ( // set and reset by ValidateArgs()
	start time.Time
	end   time.Time
)

func newCalendarAction() action.Pair {
	var aliases = []string{"entries"}

	return scaffoldlist.NewListAction(shortCalendar, longCalendar, types.CalendarEntry{}, data,
		scaffoldlist.Options{
			Use:        useCalendar,
			Aliases:    aliases,
			AddtlFlags: calendarFlags,
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() > 1 {
					return ft.InvAtMostArgN(1, uint(fs.NArg())), nil
				}
				if v, inv, err := validateTime(fs, "start"); err != nil {
					return "", err
				} else if inv != "" {
					return inv, nil
				} else {
					start = time.Date(v.Year(), v.Month(), v.Day(), 00, 00, 00, 0, time.Local)
				}
				if v, inv, err := validateTime(fs, "end"); err != nil {
					return "", err
				} else if inv != "" {
					return inv, nil
				} else {
					// end is not inclusive by default, so we set it to the very end of the given day
					end = time.Date(v.Year(), v.Month(), v.Day(), 23, 59, 59, 0, time.Local)
				}

				return "", nil
			},
		})
}

func validateTime(fs *pflag.FlagSet, flagName string) (v time.Time, invalid string, err error) {
	v = time.Now()
	s, err := fs.GetString(flagName)
	if err != nil {
		return v, "", err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return v, "", nil
	}

	// attempt to parse time
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return v, "--" + flagName + " must be a valid date formatted as YYYY-MM-DD", nil
	}
	return t, "", nil
}

func calendarFlags() pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.String("start", "", "start date for calendar stats (inclusive).\n"+
		"Must be given as YYYY-MM-DD\n"+
		"If unset, defaults to now.")
	fs.String("end", "", "end date for calendar stats (inclusive).\n"+
		"Must be given as YYYY-MM-DD\n"+
		"If unset, defaults to now.")
	fs.StringSlice("wells", nil, "specify the wells to fetch data for.\n"+
		"If unspecified, all wells will be selected.")
	return fs
}

func data(fs *pflag.FlagSet) ([]types.CalendarEntry, error) {
	// start/end are set by ValidateArgs

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
