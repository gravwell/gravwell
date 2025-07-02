package indexers

import (
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// wrapper for the the map returned by grav.GetIndexerStorageStats()
type inspectData struct {
	id string
	types.PerWellStorageStats
}

func newInspectAction() action.Pair {
	const (
		use   string = "inspect"
		short string = "get details about a specific indexer"
		long  string = "Review detailed information about a single, specified indexer"
	)
	var example = use + ft.Mandatory("xxx22024-999a-4728-94d7-d0c0703221ff")

	return scaffoldlist.NewListAction(short, long, inspectData{},
		func(fs *pflag.FlagSet) ([]inspectData, error) {
			ss, err := getInspectStats(fs)
			if err != nil {
				return nil, err
			}

			// coerce the map to an array to pass back
			var c = make([]inspectData, len(ss))
			var i uint16
			for id, stats := range ss {
				c[i] = inspectData{id, stats}
				i += 1
			}
			return c, nil
		},
		scaffoldlist.Options{Use: use, Pretty: prettyInspect, CmdMods: func(c *cobra.Command) {
			c.Example = example
		}})
}

// helper function for list dataFn and prettyInspect.
// getInspectStats parses the indexer uuid and fetches its storage stats.
func getInspectStats(fs *pflag.FlagSet) (map[string]types.PerWellStorageStats, error) {
	indexer := strings.TrimSpace(fs.Arg(0))
	// attempt to cast to uuid
	id, err := uuid.Parse(indexer)
	if err != nil {
		return nil, err
	}

	// fetch storage data
	ss, err := connection.Client.GetIndexerStorageStats(id)
	if err != nil {
		return nil, err
	} else if len(ss) < 1 {
		return nil, errors.New("did not find any indexers associated with given uuid")
	}
	return ss, nil
}

func prettyInspect(c *cobra.Command) (string, error) {
	ss, err := getInspectStats(c.Flags())
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	// format indexer storage stats
	var wells = make([]string, len(ss)) // collect keys in case --start && --end were specified
	var i uint8 = 0
	for well, stats := range ss {
		wells[i] = well
		i++
		// per-well indentation
		sb.WriteString(stylesheet.Cur.PrimaryText.Render(well))
		sb.WriteString(stylesheet.Indent + stats.Accelerator)
		// TODO format stats into sb
	}

	return sb.String(), nil
}

// TODO just split the calendar list into its own action
// TODO include extra options
/*
	scaffold.WithPositionalArguments(cobra.ExactArgs(1)),
	scaffold.WithFlagsRequiredTogether("start", "end"),
*/

/*func inspectAddtlFlags() pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.String("start", "", "start time for calendar stats.\n"+
		"May be given in RFC1123Z (Mon, 02 Jan 2006 15:04:05 -0700) or DateTime (2006-01-02 15:04:05).\n"+
		"If --start is given, --end must also be specified.")
	fs.String("end", "", "end time for calendar stats.\n"+
		"May be given in RFC1123Z (Mon, 02 Jan 2006 15:04:05 -0700) or DateTime (2006-01-02 15:04:05).\n"+
		"If --end is given, --start must also be specified.")
	return fs
}*/

/*start, err := fetchTime(fs, "start")
if err != nil {
	return nil, err
}
end, err := fetchTime(fs, "end")
if err != nil {
	return nil, err
}*/

// if a timespan was specified, also fetch and format calendar stats
/*if !start.IsZero() && !end.IsZero() {
	if err := attachCalendarStats(&sb, start, end, id, wells); err != nil {
		return nil, err
	}
}*/

// fetchTime attempts to parse the given flag as a time.Time.
// If the flag was unset, fetchTime will return time.Time{} and nil.
/*func fetchTime(fs *pflag.FlagSet, flagName string) (time.Time, error) {
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
}*/

// attachCalendarStats checks for the start and end flags. If they are found, it attaches calendar stats for the given indexer to the string builder.
// Expects the caller to validate that start and end are !zero.
/*func attachCalendarStats(sb *strings.Builder, start, end time.Time, indexer uuid.UUID, wells []string) error {
	_, err := connection.Client.GetIndexerCalendarStats(indexer, start, end, wells)
	if err != nil {
		return err
	}

	// TODO format ce into sb

	return nil
}*/
