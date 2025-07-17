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
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const useCalendar string = "calendar"

var ( // set and reset by ValidateArgs()
	start    time.Time
	end      time.Time
	idxrUUID uuid.NullUUID // should never be set if idxrName is unset
	idxrName string        // should never be set if !idxrUUID.Valid
)

func newCalendarAction() action.Pair {
	const (
		shortCalendar string = "display calendar entries"
		longCalendar  string = "Display day-by-day statistics for a given indexer and its wells or the accumulation of all wells across all indexers.\n" +
			"To fetch stats for a specific indexer, provide name or UUID as a bare argument."
	)
	var aliases = []string{"entries"}

	return scaffoldlist.NewListAction(shortCalendar, longCalendar, types.CalendarEntry{}, data,
		scaffoldlist.Options{
			Use:     useCalendar,
			Aliases: aliases,
			AddtlFlags: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.String("start", "", "start date for calendar stats (inclusive).\n"+
					"Must be given as YYYY-MM-DD\n"+
					"If unset, defaults to now.")
				fs.String("end", "", "end date for calendar stats (inclusive).\n"+
					"Must be given as YYYY-MM-DD\n"+
					"If unset, defaults to now.")
				fs.StringSlice("wells", nil, "specify the wells to fetch data for.\n"+
					"Wells must be specified by ID (ex: "+stylesheet.Cur.ExampleText.Render("a312211e-11a1-4ff4-8888-aa1a1aa11a11-default")+") or they will be ignored.\n"+
					"If unset, all wells will be selected (for the specified indexer or across all indexers).")
				return fs
			},
			// ValidateArgs does its namesake and sets/resets the package vars.
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				start, end, idxrUUID, idxrName = time.Time{}, time.Time{}, uuid.NullUUID{}, ""
				if fs.NArg() > 1 {
					return ft.InvAtMostArgN(1, uint(fs.NArg())), nil
				} else if fs.NArg() == 1 {
					if arg := strings.TrimSpace(fs.Arg(0)); arg != "" {
						name, uuid, err := identifyIndexer(arg)
						if err != nil {
							return "", err
						}
						idxrUUID.UUID = uuid
						idxrUUID.Valid = true
						idxrName = name
						clilog.Writer.Debugf("specified indexer %v (id: %v)", idxrName, idxrUUID.UUID.String())
					}
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
			CmdMods: func(c *cobra.Command) {
				c.Example = fmt.Sprintf("%v 127.0.0.1:9404 --start=1998-10-31 --wells=a311109e-63d3-4dd4-8884-da0e5cc30c33-default", useCalendar)
			},
		})
}

// helper function for ValidateArgs().
// Given a string, tries to identify the indexer name and uuid associated to it.
// If this string is a valid UUID, fetches name.
// If this string is a name, fetches UUID.
//
// ! Assumes arg has already be nil-checked and trimmed.
//
// ! This function will return UUID and name or neither, but never just one.
func identifyIndexer(arg string) (string, uuid.UUID, error) {
	idxrStats, err := connection.Client.GetIndexStats()
	if err != nil {
		return "", uuid.UUID{}, err
	}

	if id, err := uuid.Parse(arg); err == nil {
		// scan for name
		for name, stat := range idxrStats {
			if stat.UUID == id {
				return name, id, nil
			}
		}
		return "", uuid.UUID{}, fmt.Errorf("failed to find an indexer with uuid '%v'", id.String())
	}
	// assume name was given
	stats, found := idxrStats[arg]
	if !found {
		return "", uuid.UUID{}, fmt.Errorf("failed to find an indexer with name '%v'", arg)
	} else if stats.Error != "" {
		return "", uuid.UUID{}, errors.New(stats.Error)
	}
	return arg, stats.UUID, nil
}

// validateTime attempts to parse a valid DateOnly time from the given flag.
// If the flag was not specified, time.Now() is returned.
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

// actual fetch function
func data(fs *pflag.FlagSet) ([]types.CalendarEntry, error) {
	// all parameters, the globals, are managed by ValidateArgs()

	wells, err := fs.GetStringSlice("wells")
	if err != nil {
		return nil, uniques.ErrGetFlag(useCalendar, err)
	}

	// if an indexer was specified, get stats for that specific indexer
	if idxrUUID.Valid {
		clilog.Writer.Debugf("indexer=%v|start=%v|end=%v|given wells=%v", idxrUUID.UUID.String(), start, end, wells)

		// if no wells were given, get all wells associated to this indexer
		if len(wells) == 0 {
			wellData, err := connection.Client.WellData()
			if err != nil {
				return nil, err
			}
			if wellData[idxrName].UUID != idxrUUID.UUID { // sanity check
				err := fmt.Errorf("derived UUID (%v) does not match UUID of indexer associated to well data by name (%v)", idxrUUID.UUID, wellData[idxrName].UUID)
				clilog.Writer.Errorf("%v", err)
				return nil, err
			}
			wells = make([]string, len(wellData[idxrName].Wells))
			for i, w := range wellData[idxrName].Wells {
				wells[i] = w.ID
			}
		}

		return connection.Client.GetIndexerCalendarStats(idxrUUID.UUID, start, end, wells)
	}

	clilog.Writer.Debugf("start=%v|end=%v|given wells=%v", start, end, wells)

	// if no wells were given, get all wells
	if len(wells) == 0 {
		// if no wells were specified, fetch all wells from all indexers
		wellData, err := connection.Client.WellData()
		if err != nil {
			return nil, err
		}
		var (
			set sync.Map // well id -> 1
			wg  sync.WaitGroup
		)
		for _, wd := range wellData {
			wg.Add(1)
			go func(data types.IndexerWellData) {
				defer wg.Done()
				for _, well := range data.Wells {
					set.Store(well.ID, 1)
				}
			}(wd)
		}
		wg.Wait()
		set.Range(func(key, value any) bool {
			wells = append(wells, key.(string))
			return true
		})
	}

	return connection.Client.GetCalendarStats(start, end, wells)
}
