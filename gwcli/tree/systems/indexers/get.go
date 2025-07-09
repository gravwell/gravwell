package indexers

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/utils/weave"
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

	// we want to get *most* columns by default so it is easier to grab all of them and remove ones we do not want
	var defaultColumns []string
	if dcols, err := weave.StructFields(deepIndexerInfo{}, true); err != nil {
		panic("failed to delve dii: " + err.Error())
	} else {
		// add columns not in exclude to the set of default columns
		var defaultExclude = map[string]bool{
			"Ingest.EntriesHourTail":   true,
			"Ingest.EntriesMinuteTail": true,
			"Ingest.BytesHourTail":     true,
			"Ingest.BytesMinuteTail":   true,
		} // hashset
		for i := range dcols {
			if _, found := defaultExclude[dcols[i]]; !found {
				defaultColumns = append(defaultColumns, dcols[i])
			}
		}

	}

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
			Use:            use,
			DefaultColumns: defaultColumns,
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
// Allows us to massage the output format and column names.
// NOTE(rlandau): like many systems commands, these structs are based off the gravwell structs, with some variation for better print-ability.
type deepIndexerInfo struct {
	Name    string // used to retrieve most info
	UUID    string
	Storage struct {
		CoverageStart    string
		CoverageEnd      string
		DataIngestedHot  uint64
		DataIngestedCold uint64
		DataStoredHot    uint64
		DataStoredCold   uint64
		EntryCountHot    uint64
		EntryCountCold   uint64
	}
	Wells []struct {
		Name  string
		Stats types.PerWellStorageStats
	}
	Ingest   types.IngestStats
	Ping     string
	Children struct {
		Err   string
		Wells []types.IndexManagerStats
	}
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
		if state, ok := pings[dii.Name]; !ok {
			clilog.Writer.Warnf("did not find a ping state associated with indexer %v", dii.Name)
		} else {
			mu.Lock()
			dii.Ping = state
			found = true
			mu.Unlock()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		stats, err := connection.Client.GetIndexStats()
		if err != nil {
			clilog.Writer.Warnf("failed to fetch index stats: %v", err)
			return
		}
		if state, ok := stats[dii.Name]; !ok {
			clilog.Writer.Warnf("did not find a ping state associated with indexer %v", dii.Name)
		} else {
			mu.Lock()
			defer mu.Unlock()
			found = true
			// check error
			if state.Error != "" {
				dii.Children.Err = state.Error
				return
			}
			dii.Children.Wells = state.IndexStats
			dii.UUID = state.UUID.String()
		}
	}()

	// TODO fetch additional queries by name

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
		clilog.Writer.Warnf("failed to get storage stats by UUID: %v", err)
	} else if storeStats, ok := stats[dii.UUID]; ok {
		dii.Storage = struct {
			CoverageStart    string
			CoverageEnd      string
			DataIngestedHot  uint64
			DataIngestedCold uint64
			DataStoredHot    uint64
			DataStoredCold   uint64
			EntryCountHot    uint64
			EntryCountCold   uint64
		}{
			CoverageStart:    storeStats.CoverageStart.String(),
			CoverageEnd:      storeStats.CoverageEnd.String(),
			DataIngestedHot:  storeStats.DataIngestedHot,
			DataIngestedCold: storeStats.DataIngestedCold,
			DataStoredHot:    storeStats.DataStoredHot,
			DataStoredCold:   storeStats.DataStoredCold,
			EntryCountHot:    storeStats.EntryCountHot,
			EntryCountCold:   storeStats.EntryCountCold,
		}
	} else { // this is likely redundant, covered by the error above
		clilog.Writer.Infof("found no indexer with uuid %v", dii.UUID)
	}

	// fetch by parsed UUID
	parsed, err := uuid.Parse(dii.UUID)
	if err != nil {
		clilog.Writer.Infof("failed to parse %s as a UUID: %v", dii.UUID, err)
	} else {
		if stats, err := connection.Client.GetIndexerStorageStats(parsed); err != nil {
			clilog.Writer.Warnf("failed to fetch per well storage stats for indexer %v", parsed.String())
		} else {
			// transmute the map
			var st = make([]struct {
				Name  string
				Stats types.PerWellStorageStats
			}, len(stats))
			var i = 0
			for name, stat := range stats {
				st[i] = struct {
					Name  string
					Stats types.PerWellStorageStats
				}{name, stat}
				i += 1
			}

			dii.Wells = st
		}
	}
}
