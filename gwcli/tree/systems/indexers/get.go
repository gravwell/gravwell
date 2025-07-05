package indexers

import (
	"errors"
	"fmt"
	"sync"

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
			"Does not include calendar data; use the calendar action for that.\n" +
			"If an error occurs, processing will continue, skipping queries related to or cascading from the error source."
	)
	var example = fmt.Sprintf("%v xxx22024-999a-4728-94d7-d0c0703221ff", use)

	return scaffoldlist.NewListAction(short, long, deepIndexerInfo{},
		func(fs *pflag.FlagSet) ([]deepIndexerInfo, error) {
			// validate the given UUID
			idxrUUID, err := uuid.Parse(fs.Arg(0))
			if err != nil {
				return nil, err
			}

			// get as much info as we can by UUID
			dii, err := fetchByUUID(idxrUUID)
			if err != nil {
				return nil, err
			}

			fetchByName(&dii)

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
	Ingest  types.IngestStats
	Ping    string
}

// fetchbyUUID fetches all indexer info that can be plumbed via UUID.
// An error is only returned if it was a critical error (specifically being unable to find an indexer associated to the UUID).
// Logs for the caller; no need to log the returned error.
func fetchByUUID(idxrUUID uuid.UUID) (dii deepIndexerInfo, _ error) {
	// fetch storage stats by UUID
	if stats, err := connection.Client.GetStorageStats(); err != nil {
		// TODO check for no-indexer-found error
		return dii, err
	} else if storeStats, ok := stats[dii.UUID]; ok {
		dii.Storage = storeStats
	} else { // this is likely redundant, covered by the error above
		clilog.Writer.Infof("found no indexer with uuid %v", idxrUUID.String())
		return dii, errors.New("found no indexer associated with uuid " + idxrUUID.String())
	}

	if stats, err := connection.Client.GetIndexerStorageStats(idxrUUID); err != nil {
		clilog.Writer.Warnf("failed to fetch per well storage stats for indexer %v", idxrUUID.String())
	} else {
		dii.Wells = stats
	}

	// we need to pivot off the indexer name, as most other information is fetched by name, not UUID
	if stats, err := connection.Client.GetIndexStats(); err != nil {
		clilog.Writer.Warnf("failed to fetch per well storage stats for indexer %v", idxrUUID.String())
	} else {
		// walk through the list of indexers and find a matching UUID
		for idxrName, idxrStats := range stats {
			if idxrStats.UUID == idxrUUID {
				// found a match, save off the info (and the name for further querying)
				dii.Name = idxrName
				// TODO attach the stats array
			}
		}
	}
	return dii, nil
}

// Using dii.Name, fetches as much information as can be queried by name.
// Updates the given dii.
// Returns immediately if dii.Name is empty.
// Logs and swallows errors, leaving the related fields empty.
func fetchByName(dii *deepIndexerInfo) {
	if dii.Name == "" {
		return
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	// get ingesters
	wg.Add(1)
	go func() {
		defer wg.Done()
		ingStats, err := connection.Client.GetIngesterStats()
		if err != nil {
			clilog.Writer.Warnf("failed to fetch ingester stats: %v", err)
			return
		}
		for idxr, stat := range ingStats {
			if idxr != dii.Name {
				continue
			}
			mu.Lock()
			dii.Ingest = stat
			mu.Unlock()
			return
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		pings, err := connection.Client.GetPingStates()
		if err != nil {
			clilog.Writer.Warnf("failed to fetch ping states: %v", err)
			return
		}
		for idxrName, pingState := range pings {
			if idxrName != dii.Name {
				continue
			}
			mu.Lock()
			dii.Ping = pingState
			mu.Unlock()
		}
	}()

	// TODO fetch additional queried by name

	wg.Wait()

}
