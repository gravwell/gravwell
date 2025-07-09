package indexers

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
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
	)

	var (
		long = "Review detailed information about a single indexer.\n" +
			"Does not include calendar data; use the calendar action for that.\n" +
			"Indexer is specified by name; use the " + stylesheet.Cur.Nav.Render("indexers") + " " + stylesheet.Cur.Action.Render("list") + " command.\n" +
			"If an error occurs, processing will continue, skipping queries related to or cascading from the error source."
		example = fmt.Sprintf("%v 127.0.0.1:9404", use)
	)

	return scaffoldlist.NewListAction(short, long, deepIndexerInfo{},
		func(fs *pflag.FlagSet) ([]deepIndexerInfo, error) {
			dii := deepIndexerInfo{Name: strings.TrimSpace(fs.Arg(0))}

			if !dii.fetchByName() {
				return nil, errors.New("found no data related to indexer '" + dii.Name + "'")
			}
			dii.fetchByUUID()

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
					return "exactly 1 argument (name) is required", nil
				}
				// preliminary existence query
				if m, err := connection.Client.GetPingStates(); err != nil {
					return "", err
				} else if _, found := m[fs.Arg(0)]; !found {
					return "did not find indexer '" + fs.Arg(0) + "'", nil
				}
				return "", nil
			},
		})
}

// deepIndexerInfo collects all information available about a single indexer.
// Most data is queried by name, though we must pivot on UUID for a couple calls.
type deepIndexerInfo struct {
	Name    string // used to retrieve most info
	UUID    string
	Storage types.StorageStats
	Wells   map[string]types.PerWellStorageStats // can we use a map? // TODO
	Ingest  types.IngestStats
	Ping    string
}

// DESTRUCTIVELY ALTERS DII.
//
// Using dii.Name, fetches as much information as can be queried by name and installs it into dii.
// Returns immediately if dii.Name is empty.
// fetchByName logs and swallows errors, leaving the related fields empty.
// Only returns false if ALL queries failed to find the named indexer.
func (dii *deepIndexerInfo) fetchByName() (found bool) {
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
		mu.Lock()
		found = true
		mu.Unlock()
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
		mu.Lock()
		found = true
		mu.Unlock()
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

	return
}

// DESTRUCTIVELY ALTERS DII.
//
// Pivoting on dii.UUID, fetches as much information as can be queried by uuid and installs it into dii.
// Returns immediately if dii.UUID is empty.
// Logs and swallows errors, leaving the related fields empty.
func (dii *deepIndexerInfo) fetchByUUID() {
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
