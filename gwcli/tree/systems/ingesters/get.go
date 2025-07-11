package ingesters

import (
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	flagHostname string = "hostname"
	flagUUID     string = "uuid"
	flagName     string = "name"
)

// list generates an action for retrieving information about the ingesters.
func get() action.Pair {
	const (
		use   string = "get"
		short string = "get all info about a subset of ingesters"
		long  string = "Get detailed information about one or several ingesters by prefix-matching on their attributes\n" +
			"You must specify at least one of --" + flagHostname + ", --" + flagUUID + ", --" + flagName + " is required"
	)

	return scaffoldlist.NewListAction(short, long, wrappedIngesterStats{},
		func(fs *pflag.FlagSet) ([]wrappedIngesterStats, error) {
			// check that we were given ingesters to fetch
			hostPrefix, err := fs.GetString(flagHostname)
			if err != nil {
				return nil, uniques.ErrGetFlag(use, err)
			}
			uuidPrefix, err := fs.GetString(flagUUID)
			if err != nil {
				return nil, uniques.ErrGetFlag(use, err)
			}
			namePrefix, err := fs.GetString(flagName)
			if err != nil {
				return nil, uniques.ErrGetFlag(use, err)
			}

			ss, err := connection.Client.GetIngesterStats()
			if err != nil {
				return nil, err
			}

			// ingesters are stored by uuid
			ingesters := map[string]wrappedIngesterStats{}

			// find the ingesters
			for idxr, idxrStats := range ss { // walk each indexer
				for _, ingstrStats := range idxrStats.Ingesters { // walk each ingester
					// if we get any prefix match, note this ingester
					if nonEmptyPrefixMatch(ingstrStats.State.Hostname, hostPrefix) ||
						nonEmptyPrefixMatch(ingstrStats.State.UUID, uuidPrefix) ||
						nonEmptyPrefixMatch(ingstrStats.State.Name, namePrefix) {
						insertNoOverride(ingesters, newWrapped(idxr, ingstrStats))
					}
				}
			}

			// drop keys
			return slices.Collect(maps.Values(ingesters)), nil
		}, scaffoldlist.Options{
			Use: use,
			AddtlFlags: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.String(flagHostname, "", "prefix-match ingesters on hostname")
				fs.String(flagUUID, "", "prefix-match ingesters on uuid")
				fs.String(flagName, "", "prefix-match ingesters on name")
				return fs
			},
			CmdMods: func(c *cobra.Command) {
				c.Example = fmt.Sprintf("%v --%s=12345", flagHostname, use)
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				// validate that at least one of the additional flags was given
				// check that we were given ingesters to fetch
				hostPrefix, err := fs.GetString("hostname")
				if err != nil {
					return "", uniques.ErrGetFlag(use, err)
				}
				uuidPrefix, err := fs.GetString("uuid")
				if err != nil {
					return "", uniques.ErrGetFlag(use, err)
				}
				namePrefix, err := fs.GetString("name")
				if err != nil {
					return "", uniques.ErrGetFlag(use, err)
				}

				// we cannot use c.MarkFlagsOneRequired("hostname", "uuid", "name") as it will not be factored into SetArgs
				if hostPrefix == "" && uuidPrefix == "" && namePrefix == "" {
					return fmt.Sprintf("at least one of --%s, --%s, --%s is required", flagHostname, flagUUID, flagName), nil
				}
				return "", nil
			},
		})
}

// wrapper around types.IngesterStats for clearer columns names
type wrappedIngesterStats struct {
	Indexer           string
	RemoteAddress     string
	Size              uint64
	Uptime            string // time.Duration
	Tags              []string
	Name              string
	Version           string
	UUID              string
	Label             string
	IP                net.IP
	Hostname          string
	Entries           uint64 // appears to be equal to types.IngesterStats.Count
	StateSize         uint64
	CacheState        string
	CacheSize         uint64
	LastSeen          time.Time
	Children          []string
	ConfigurationJSON string
	MetadataJSON      string
}

// Returns a new wrappedIngesterStats instance.
func newWrapped(indexer string, stats types.IngesterStats) wrappedIngesterStats {
	cfg, err := stats.State.Configuration.MarshalJSON()
	if err != nil {
		clilog.Writer.Warnf("failed to marshal configuration while wrapping: %v", err)
	}
	mtdta, err := stats.State.Metadata.MarshalJSON()
	if err != nil {
		clilog.Writer.Warnf("failed to marshal metadata while wrapping: %v", err)
	}
	w := wrappedIngesterStats{
		Indexer:           indexer,
		RemoteAddress:     stats.RemoteAddress,
		Size:              stats.Size,
		Uptime:            stats.Uptime.String(),
		Tags:              stats.Tags,
		Name:              stats.Name,
		Version:           stats.Version,
		UUID:              stats.UUID,
		Label:             stats.State.Label,
		IP:                stats.State.IP,
		Hostname:          stats.State.Hostname,
		Entries:           stats.State.Entries,
		StateSize:         stats.State.Size,
		CacheState:        stats.State.CacheState,
		CacheSize:         stats.State.CacheSize,
		LastSeen:          stats.State.LastSeen,
		Children:          slices.Collect(maps.Keys(stats.State.Children)),
		ConfigurationJSON: string(cfg),
		MetadataJSON:      string(mtdta),
	}
	return w
}

// Only inserts the given uuid -> stats if the uuid does not currently exist in the map
func insertNoOverride(ingesters map[string]wrappedIngesterStats, ing wrappedIngesterStats) {
	_, found := ingesters[ing.UUID]
	if found {
		return
	}
	ingesters[ing.UUID] = ing
}

// Returns true iff s is not empty and begins with prefix.
func nonEmptyPrefixMatch(s, prefix string) bool {
	if s == "" || prefix == "" {
		return false
	}
	return strings.HasPrefix(s, prefix)
}
