/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/* Package indexers defines a nav for status actions related to the indexers. */
package indexers

import (
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/tree/status/indexers/storage"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/gravwell/gravwell/v4/utils/weave"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	use   string = "indexers"
	short string = "view indexer status"
	long  string = "Review the status, storage, and state of indexers associated to your instance."
)

var aliases []string = []string{"index", "idx", "indexer"}

func NewIndexersNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{},
		[]action.Pair{
			storage.NewIndexerStorageAction(),
			newStatsListAction(),
			newInspectBasicAction(),
		})
}

//#region stats

// wrapper for the SysStats map
type namedStats struct {
	Indexer string
	Stats   types.HostSysStats
}

func newStatsListAction() action.Pair {
	const (
		use   string = "stats"
		short string = "review the statistics of each indexer"
		long  string = "Review the statistics of each indexer"
	)
	// default to using all columns; dive into the struct to find all columns
	cols, err := weave.StructFields(namedStats{}, true)
	if err != nil {
		panic(err)
	}

	return scaffoldlist.NewListAction(use, short, long, cols,
		namedStats{}, listStats, nil)
}

func listStats(c *grav.Client, fs *pflag.FlagSet) ([]namedStats, error) {
	var ns []namedStats

	stats, err := connection.Client.GetSystemStats()
	if err != nil {
		return []namedStats{}, err
	}
	ns = make([]namedStats, len(stats))

	// wrap the results in namedStats
	var i = 0
	for k, v := range stats {
		ns[i] = namedStats{Indexer: k, Stats: *v.Stats}
		i += 1
	}

	return ns, nil
}

//#endregion stats

//#region inspect

func newInspectBasicAction() action.Pair {
	const (
		use   string = "inspect"
		short string = "get details about a specific indexer"
		long  string = "Review detailed information about a single, specified indexer"
	)

	return scaffold.NewBasicAction(use, short, long, []string{"details"}, func(c *cobra.Command) (string, tea.Cmd) {
		start, err := fetchTime(c, "start")
		if err != nil {
			return stylesheet.Cur.ErrorText.Render(err.Error()), nil
		}
		end, err := fetchTime(c, "end")
		if err != nil {
			return stylesheet.Cur.ErrorText.Render(err.Error()), nil
		}

		indexer := strings.TrimSpace(c.Flags().Arg(0))
		// attempt to cast to uuid
		id, err := uuid.Parse(indexer)
		if err != nil {
			return stylesheet.Cur.ErrorText.Render(err.Error()), nil
		}

		// fetch storage data
		ss, err := connection.Client.GetIndexerStorageStats(id)
		if err != nil {
			return stylesheet.Cur.ErrorText.Render(err.Error()), nil
		} else if len(ss) < 1 {
			return stylesheet.Cur.ErrorText.Render("did not find any indexers associated with given uuid"), nil
		}

		var sb strings.Builder
		// format indexer storage stats
		var wells []string = make([]string, len(ss)) // collect keys in case --start && --end were specified
		var i uint8 = 0
		for well, _ := range ss {
			wells[i] = well
			i++
			// TODO format stats into sb
		}

		// if a timespan was specified, also fetch and format calendar stats
		if !start.IsZero() && !end.IsZero() {
			if err := attachCalendarStats(&sb, start, end, id, wells); err != nil {
				return stylesheet.Cur.ErrorText.Render(err.Error()), nil
			}
		}

		return sb.String(), nil
	}, func() pflag.FlagSet {
		fs := pflag.FlagSet{}
		fs.String("start", "", "start time for calendar stats.\n"+
			"May be given in RFC1123Z (Mon, 02 Jan 2006 15:04:05 -0700) or DateTime (2006-01-02 15:04:05).\n"+
			"If --start is given, --end must also be specified.")
		fs.String("end", "", "end time for calendar stats.\n"+
			"May be given in RFC1123Z (Mon, 02 Jan 2006 15:04:05 -0700) or DateTime (2006-01-02 15:04:05).\n"+
			"If --end is given, --start must also be specified.")
		return fs
	},
		scaffold.WithExample("gwcli status indexers inspect xxx22024-999a-4728-94d7-d0c0703221ff"),
		scaffold.WithPositionalArguments(cobra.ExactArgs(1)),
		scaffold.WithFlagsRequiredTogether("start", "end"),
	)

}

// fetchTime attempts to parse the given flag as a time.Time.
// If the flag was unset, fetchTime will return time.Time{} and nil.
func fetchTime(c *cobra.Command, flagName string) (time.Time, error) {
	// check for and parse start and end flags
	s, err := c.Flags().GetString(flagName)
	if err != nil {
		return time.Time{}, uniques.ErrFlagDNE("start", "inspect")
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

// attachCalendarStats checks for the start and end flags. If they are found, it attaches calendar stats for the given indexer to the string builder.
// Expects the caller to validate that start and end are !zero.
func attachCalendarStats(sb *strings.Builder, start, end time.Time, indexer uuid.UUID, wells []string) error {
	_, err := connection.Client.GetIndexerCalendarStats(indexer, start, end, wells)
	if err != nil {
		return err
	}

	// TODO format ce into sb
	return nil
}

//#endregion inspect
