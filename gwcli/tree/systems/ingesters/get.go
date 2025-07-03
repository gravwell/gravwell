package ingesters

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// list generates an action for retrieving information about the ingesters.
func get() action.Pair {
	const (
		use   string = "get"
		short string = "review info about all ingesters"
		long  string = "Get detailed information about one or several ingesters by prefix-matching on their attributes\n" +
			"You must specify at least one of --hostname, --uuid, or --name."
	)

	return scaffoldlist.NewListAction(short, long, wrappedIngesterStats{},
		func(fs *pflag.FlagSet) ([]wrappedIngesterStats, error) {
			// check that we were given ingesters to fetch
			hostPrefix, err := fs.GetString("hostname")
			if err != nil {
				return nil, uniques.ErrGetFlag(use, err)
			}
			uuidPrefix, err := fs.GetString("uuid")
			if err != nil {
				return nil, uniques.ErrGetFlag(use, err)
			}
			namePrefix, err := fs.GetString("name")
			if err != nil {
				return nil, uniques.ErrGetFlag(use, err)
			}

			// we cannot use c.MarkFlagsOneRequired("hostname", "uuid", "name") as it will not be factored into SetArgs
			if hostPrefix == "" && uuidPrefix == "" && namePrefix == "" {
				return nil, errors.New("at least one of --hostname, --uuid, --name is required")
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
					if strings.HasPrefix(ingstrStats.State.Hostname, hostPrefix) ||
						strings.HasPrefix(ingstrStats.State.UUID, uuidPrefix) ||
						strings.HasPrefix(ingstrStats.State.Name, namePrefix) {
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
				fs.String("hostname", "", "prefix-match ingesters on hostname")
				fs.String("uuid", "", "prefix-match ingesters on uuid")
				fs.String("name", "", "prefix-match ingesters on name")
				return fs
			},
			//DefaultColumns: ,
			CmdMods: func(c *cobra.Command) {
				c.Example = fmt.Sprintf("%v --hostname=176.1 --name=web", use)
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
					return "at least one of --hostname, --uuid, --name is required", nil
				}
				return "", nil
			},
		})
}

// wrapper around types.IngesterStats for clearer columns names
type wrappedIngesterStats struct {
	Indexer       string
	RemoteAddress string
	Count         uint64
	Size          uint64
	Uptime        string
	Tags          []string
	Name          string
	Version       string
	UUID          string
	StateUUID     string
	StateName     string
	StateVersion  string
	Label         string
	IP            net.IP
	Hostname      string
	Entries       uint64
	StateSize     uint64
	StateUptime   string // time.Duration
	StateTags     []string
	CacheState    string
	CacheSize     uint64
	LastSeen      time.Time
	Children      []string
	Configuration json.RawMessage `json:",omitempty"`
	Metadata      json.RawMessage `json:",omitempty"`
}

// Returns a new wrappedIngesterStats instance.
func newWrapped(indexer string, stats types.IngesterStats) wrappedIngesterStats {
	w := wrappedIngesterStats{
		Indexer:       indexer,
		RemoteAddress: stats.RemoteAddress,
		Count:         stats.Count,
		Size:          stats.Size,
		Uptime:        stats.Uptime.String(),
		Tags:          stats.Tags,
		Name:          stats.Name,
		Version:       stats.Version,
		UUID:          stats.UUID,
		StateUUID:     stats.State.UUID,
		StateName:     stats.State.Name,
		StateVersion:  stats.State.Version,
		Label:         stats.State.Label,
		IP:            stats.State.IP,
		Hostname:      stats.State.Hostname,
		Entries:       stats.State.Entries,
		StateSize:     stats.State.Size,
		StateUptime:   stats.State.Uptime.String(),
		StateTags:     stats.State.Tags,
		CacheState:    stats.State.CacheState,
		CacheSize:     stats.State.CacheSize,
		LastSeen:      stats.State.LastSeen,
		Children:      slices.Collect(maps.Keys(stats.State.Children)),
		Configuration: stats.State.Configuration,
		Metadata:      stats.State.Metadata,
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
