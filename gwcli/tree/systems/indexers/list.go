/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package indexers

import (
	"maps"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/spf13/pflag"
)

type list_t struct {
	Name             string // IP address or "webserver", typically
	UUID             string
	Ping             string
	Wells            []string
	Storage          types.StorageStats
	Ingesters        []string // ingester names
	MissingIngesters []string
}

// list samples a data about indexers from a range of API calls, coalescing the data into list_t.
func list() action.Pair {
	const (
		short string = "review info about all indexers"
		long  string = "Review general statistics about all indexers."
	)

	return scaffoldlist.NewListAction(short, long, list_t{},
		func(fs *pflag.FlagSet) ([]list_t, error) {
			// keep the info in a map by indexer for better looking
			m := map[string]*list_t{} // name (IP/"webserver") -> info

			// ping returns name/IP -> stats
			if pings, err := connection.Client.GetPingStates(); err != nil {
				return nil, err
			} else {
				clilog.Writer.Debugf("Found %d ping states", len(pings))
				for idxr, ping := range pings {
					if addIndexer(m, idxr) {
						continue
					}
					m[idxr].Ping = ping
				}
			}

			// indexStats returns name/IP -> stats
			if stats, err := connection.Client.GetIndexStats(); err != nil {
				return nil, err
			} else {
				clilog.Writer.Debugf("Found %d index states", len(stats))
				for idxr, stat := range stats {
					if addIndexer(m, idxr) {
						continue
					}
					if stat.Error != "" {
						clilog.Writer.Warnf("failed to fetch index stats for indexer %v", idxr)
						continue
					}
					m[idxr].UUID = stat.UUID.String()
					var w = make([]string, len(stat.IndexStats))
					for i, wellInfo := range stat.IndexStats {
						w[i] = wellInfo.Name
					}
					m[idxr].Wells = w
					// stable sort Wells, as the API's list is unstable
					slices.SortStableFunc(m[idxr].Wells, strings.Compare)
				}
			}

			// storage stats returns info by UUID
			if stats, err := connection.Client.GetStorageStats(); err != nil {
				return nil, err
			} else {
				for uuid, stat := range stats {
					// because it is returned by uuid, we have to manually scan for the matching UUID
					for idxrName, idxrInfo := range m {
						if idxrInfo.UUID == uuid {
							// set the info
							if addIndexer(m, idxrName) {
								break
							}
							m[idxrName].Storage = stat
							// move to the next uuid
							break
						}
					}
				}
			}

			// ingester stats are returned data by indexer
			if ingStats, err := connection.Client.GetIngesterStats(); err != nil {
				return nil, err
			} else {
				for idxr, stat := range ingStats {
					if addIndexer(m, idxr) {
						continue
					}
					{
						ingesterNames := make([]string, len(stat.Ingesters))
						for i, ing := range stat.Ingesters {
							ingesterNames[i] = ing.Name
						}
						m[idxr].Ingesters = ingesterNames
					}
					{
						missingIngesterNames := make([]string, len(stat.Missing))
						for i, ing := range stat.Missing {
							missingIngesterNames[i] = ing.Name
						}
						m[idxr].MissingIngesters = missingIngesterNames
					}
				}
			}

			// dereference each item, now that it has been built
			itr := maps.Values(m)
			var ret = make([]list_t, len(m))
			var i = 0
			for v := range itr {
				ret[i] = *v
				clilog.Writer.Debugf("%v", ret[i])
				i += 1
			}

			return ret, nil
		},
		scaffoldlist.Options{})
}

// Inserts the indexer into the map if it does not already exist.
// Ignores 'webserver'.
func addIndexer(m map[string]*list_t, name string) (skip bool) {
	if name == "webserver" {
		return true
	} else if _, ok := m[name]; ok {
		return false
	}
	m[name] = &list_t{Name: name}
	return false
}
