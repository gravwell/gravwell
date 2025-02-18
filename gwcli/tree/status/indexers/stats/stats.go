package stats

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/utils/weave"
	"github.com/spf13/pflag"
)

const (
	use   string = "stats"
	short string = "review the statistics of each indexer"
	long  string = "Review the statistics of each indexer"
)

// wrapper for the SysStats map
type namedStats struct {
	Indexer string
	Stats   types.HostSysStats
}

func NewStatsListAction() action.Pair {
	// default to using all columns; dive into the struct to find all columns
	cols, err := weave.StructFields(namedStats{}, true)
	if err != nil {
		panic(err)
	}

	return scaffoldlist.NewListAction(use, short, long, cols,
		namedStats{}, list, nil)
}

func list(c *grav.Client, fs *pflag.FlagSet) ([]namedStats, error) {
	var ns []namedStats

	stats, err := connection.Client.GetSystemStats()
	if err != nil {
		return []namedStats{}, err
	}
	ns = make([]namedStats, len(stats))

	// wrap the results in namedStats
	var i int = 0
	for k, v := range stats {
		ns[i] = namedStats{Indexer: k, Stats: *v.Stats}
		i += 1
	}

	return ns, nil
}
