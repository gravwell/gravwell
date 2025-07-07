/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package indexers contains actions for fetching information about the state of the indexers.
package indexers

import (
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewIndexersNav() *cobra.Command {
	const (
		use   string = "indexers"
		short string = "view indexer status"
	)

	var long = "Review the health, storage, configuration, and history of indexers. Use " + stylesheet.Cur.Action.Render("list")

	var aliases = []string{"index", "idx", "indexer"}

	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{},
		[]action.Pair{
			newStatsListAction(),
			get(),
			list(),
			newCalendarAction(),
		})
}

//#region stats

// wrapper for the SysStats map.
// Allows us to omit a prefix for the embedded type.
type wrappedSysStats struct {
	Indexer               string
	Uptime                uint64
	TotalMemory           uint64
	ProcessHeapAllocation uint64 // bytes allocated by this process's heap
	ProcessSysReserved    uint64 // total bytes obtained from the OS
	MemoryUsedPercent     float64
	HostHash              string
	Net                   types.NetworkUsage
	VirtSystem            string          // e.g. "kvm" or "xen"
	VirtRole              string          // "host" or "guest"
	BuildInfo             types.BuildInfo // e.g. 3.3.1
	CanonicalVersion      string
	Iowait                float64
}

func newStatsListAction() action.Pair {
	const (
		use   string = "stats"
		short string = "review the statistics of each indexer"
		long  string = "Review the statistics of each indexer"
	)

	return scaffoldlist.NewListAction(
		short, long,
		wrappedSysStats{}, listStats, scaffoldlist.Options{
			Use:            use,
			Pretty:         nil, // TODO
			DefaultColumns: []string{"Indexer", "Uptime", "TotalMemory", "VirtSystem", "VirtRole", "CanonicalVersion"},
		})
}

func listStats(fs *pflag.FlagSet) ([]wrappedSysStats, error) {
	var ns []wrappedSysStats

	stats, err := connection.Client.GetSystemStats()
	if err != nil {
		return []wrappedSysStats{}, err
	}
	ns = make([]wrappedSysStats, len(stats))

	// wrap the results
	var i = 0
	for k, v := range stats {
		ns[i] = wrappedSysStats{
			Indexer:               k,
			Uptime:                v.Stats.Uptime,
			TotalMemory:           v.Stats.TotalMemory,
			ProcessHeapAllocation: v.Stats.ProcessHeapAllocation,
			ProcessSysReserved:    v.Stats.ProcessSysReserved,
			MemoryUsedPercent:     v.Stats.MemoryUsedPercent,
			HostHash:              v.Stats.HostHash,
			Net:                   v.Stats.Net,
			VirtSystem:            v.Stats.VirtSystem,
			VirtRole:              v.Stats.VirtRole,
			BuildInfo:             v.Stats.BuildInfo,
			CanonicalVersion:      v.Stats.BuildInfo.CanonicalVersion.String(),
			Iowait:                v.Stats.Iowait,
		}
		i += 1
	}

	return ns, nil
}

//#endregion stats
