package indexers

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
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
		long  string = "Review detailed information about a single, specified indexer.\n" +
			"Does not include calendar data; use the calendar action for that."
	)
	var example = fmt.Sprintf("%v xxx22024-999a-4728-94d7-d0c0703221ff", use)

	return scaffoldlist.NewListAction(short, long, deepIndexerInfo{},
		func(fs *pflag.FlagSet) ([]deepIndexerInfo, error) {
			// validate the given UUID
			idxrUUID, err := uuid.Parse(fs.Arg(0))
			if err != nil {
				return nil, err
			}

			dii := deepIndexerInfo{UUID: idxrUUID.String()}

			// fetch storage stats by UUID
			if stats, err := connection.Client.GetStorageStats(); err != nil {
				return nil, err
			} else if storeStats, ok := stats[dii.UUID]; ok {
				dii.Storage = storeStats
			} else {
				clilog.Writer.Infof("found no indexer with uuid %v", idxrUUID.String())
				return nil, errors.New("found no indexer associated with uuid " + idxrUUID.String())
			}

			if stats, err := connection.Client.GetIndexerStorageStats(idxrUUID); err != nil {
				clilog.Writer.Warnf("failed to fetch per well storage stats for indexer %v", idxrUUID.String())
			} else {
				dii.Wells = stats
			}

			return []deepIndexerInfo{dii}, nil
		},
		scaffoldlist.Options{
			Use: use,
			//Pretty: prettyInspect,
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
	UUID    string // our initial pivot point
	Name    string // used to retrieve most info
	Storage types.StorageStats
	Wells   map[string]types.PerWellStorageStats // can we use a map? // TODO
}
