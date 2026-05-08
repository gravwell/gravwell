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
			ExcludeColumnsFromDefault: []string{
				"Ingest.EntriesHourTail",
				"Ingest.EntriesMinuteTail",
				"Ingest.BytesHourTail",
				"Ingest.BytesMinuteTail",
			},
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
			ColumnAliases: map[string]string{
				"Storage.DataIngestedHot":  "Storage.Hot.Ingested",
				"Storage.DataIngestedCold": "Storage.Cold.Ingested",
				"Storage.DataStoredHot":    "Storage.Hot.Stored",
				"Storage.DataStoredCold":   "Storage.Cold.Stored",
				"Storage.EntryCountHot":    "Storage.Hot.Count",
				"Storage.EntryCountCold":   "Storage.Cold.Count",
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
	Ingest struct { // based on types.IngestStats, but with Ingesters removed
		QuotaUsed         uint64     // Quota used so far
		QuotaMax          uint64     // Total quota
		EntriesPerSecond  float64    // Entries per second over the last few seconds
		BytesPerSecond    float64    // Bytes per second over the last few seconds
		TotalCount        uint64     //Total Entries since the ingest server started
		TotalSize         uint64     //Total Data since the ingest server started
		LastDayCount      uint64     //total entries in last 24 hours
		LastDaySize       uint64     //total ingested in last 24 hours
		EntriesHourTail   [24]uint64 //entries per 1 hour bucket with 24 hours of tail
		EntriesMinuteTail [60]uint64 //entries per 1 second bucket with 60s of tail
		BytesHourTail     [24]uint64 //bytes per 1 hour bucket with 24 hours of tail
		BytesMinuteTail   [60]uint64 //bytes per 1 second bucket with 60s of tail
		Ingesters         []struct { // based on types.IngesterStats
			RemoteAddress string
			UUID          string
			Name          string
		}
		Missing []struct { // ingesters that have been seen before but not actively connected now
			UUID     string
			Name     string
			LastSeen string
		}
	}
	Ping     string
	Children struct {
		Err   string
		Wells []types.IndexManagerStats
	}
	System struct {
		Description types.SysInfo
		Stats       types.SysStats
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
			dii.Ingest = struct {
				QuotaUsed         uint64
				QuotaMax          uint64
				EntriesPerSecond  float64
				BytesPerSecond    float64
				TotalCount        uint64
				TotalSize         uint64
				LastDayCount      uint64
				LastDaySize       uint64
				EntriesHourTail   [24]uint64
				EntriesMinuteTail [60]uint64
				BytesHourTail     [24]uint64
				BytesMinuteTail   [60]uint64
				Ingesters         []struct {
					RemoteAddress string
					UUID          string
					Name          string
				}
				Missing []struct {
					UUID     string
					Name     string
					LastSeen string
				}
			}{
				QuotaUsed:         stat.QuotaUsed,
				QuotaMax:          stat.QuotaMax,
				EntriesPerSecond:  stat.EntriesPerSecond,
				BytesPerSecond:    stat.BytesPerSecond,
				TotalCount:        stat.TotalCount,
				TotalSize:         stat.TotalSize,
				LastDayCount:      stat.LastDayCount,
				LastDaySize:       stat.LastDaySize,
				EntriesHourTail:   stat.EntriesHourTail,
				EntriesMinuteTail: stat.EntriesMinuteTail,
				BytesHourTail:     stat.BytesHourTail,
				BytesMinuteTail:   stat.BytesMinuteTail,
				Ingesters: make([]struct {
					RemoteAddress string
					UUID          string
					Name          string
				}, len(stat.Ingesters)),
				Missing: make([]struct {
					UUID     string
					Name     string
					LastSeen string
				}, len(stat.Missing)),
			}
			for i, ing := range stat.Ingesters {
				dii.Ingest.Ingesters[i] = struct {
					RemoteAddress string
					UUID          string
					Name          string
				}{
					ing.RemoteAddress, ing.UUID, ing.Name,
				}
			}
			for i, ing := range stat.Missing {
				dii.Ingest.Missing[i] = struct {
					UUID     string
					Name     string
					LastSeen string
				}{
					ing.UUID, ing.Name, ing.LastSeen.String(),
				}
			}
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
			clilog.Writer.Warnf("did not find an index state associated with indexer %v", dii.Name)
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

	wg.Add(1)
	go func() {
		defer wg.Done()
		descs, err := connection.Client.GetSystemDescriptions()
		if err != nil {
			clilog.Writer.Warnf("failed to fetch system descriptions: %v", err)
			return
		}
		if desc, ok := descs[dii.Name]; !ok {
			clilog.Writer.Warnf("did not find a system description associated with indexer %v", dii.Name)
		} else {
			mu.Lock()
			defer mu.Unlock()
			found = true
			dii.System.Description = desc
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		stats, err := connection.Client.GetSystemStats()
		if err != nil {
			clilog.Writer.Warnf("failed to fetch system descriptions: %v", err)
			return
		}
		if stat, ok := stats[dii.Name]; !ok {
			clilog.Writer.Warnf("did not find a system description associated with indexer %v", dii.Name)
		} else {
			mu.Lock()
			defer mu.Unlock()
			found = true
			dii.System.Stats = stat
		}
	}()
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
