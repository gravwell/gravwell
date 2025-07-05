package indexers

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// get fetches all available information about the named ingester.
// Currently requires a UUID, but could be upgraded to prefix match, like ingesters.
func get() action.Pair {
	const (
		use   string = "get"
		short string = "get details about a specific indexer"
		long  string = "Review detailed information about a single, specified indexer"
	)
	var example = fmt.Sprintf("%v xxx22024-999a-4728-94d7-d0c0703221ff", use)

	return scaffoldlist.NewListAction(short, long, deepIndexerInfo{},
		func(fs *pflag.FlagSet) ([]deepIndexerInfo, error) {
			ss, err := getInspectStats(fs)
			if err != nil {
				return nil, err
			}

			// coerce the map to an array to pass back
			var c = make([]deepIndexerInfo, len(ss))
			var i uint16
			for id, stats := range ss {
				c[i] = deepIndexerInfo{id, stats}
				i += 1
			}
			return c, nil
		},
		scaffoldlist.Options{
			Use:    use,
			Pretty: prettyInspect,
			CmdMods: func(c *cobra.Command) {
				c.Example = example
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return "exactly 1 argument (UUID) is required", nil
				}
				return "", nil
			},
		})
}

// wrapper for the the map returned by grav.GetIndexerStorageStats()
type deepIndexerInfo struct {
	id string
	types.PerWellStorageStats
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
