/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/* Package systemshealth defines a nav for actions related to the status of the backend. */
package systemshealth

import (
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/tree/systems/indexers"
	"github.com/gravwell/gravwell/v4/gwcli/tree/systems/ingesters"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewSystemsNav() *cobra.Command {
	const (
		use   string = "systems"
		short string = "systems and health of the instance"
		long  string = "Review the state and health of your system."
	)
	var aliases = []string{"health", "status", "system"}
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{
			indexers.NewIndexersNav(),
			ingesters.NewIngestersNav(),
		},
		[]action.Pair{
			newStorageAction(),
			newHardwareAction(),
			state(),
		})
}

//#region storage
// a basic action for fetching indexer storage info.

// wrapper for the map returned by GetStorageStats.
type namedStorage struct {
	Disk  string
	Stats types.StorageStats
}

// Generates a list action that returns the storage statistics of all indexers in the Gravwell instance.
func newStorageAction() action.Pair {
	const (
		use   string = "storage"
		short string = "review storage statistics"
		long  string = "Fetch instance-wide storage statistics.\n" +
			"All data is in bytes, unless otherwise marked."
	)

	return scaffoldlist.NewListAction(short, long, namedStorage{},
		func(fs *pflag.FlagSet) ([]namedStorage, error) {
			ss, err := connection.Client.GetStorageStats()
			if err != nil {
				return []namedStorage{}, err
			}
			var wrap = make([]namedStorage, len(ss))
			var i = 0
			for disk, stats := range ss {
				wrap[i] = namedStorage{Disk: disk, Stats: stats}
				i += 1
			}

			return wrap, nil
		}, scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{Use: use},
			// should match the aliases used in the systems indexers list action
			ColumnAliases: map[string]string{
				"Stats.DataIngestedHot":  "Hot.Ingested",
				"Stats.DataIngestedCold": "Cold.Ingested",
				"Stats.DataStoredHot":    "Hot.Stored",
				"Stats.DataStoredCold":   "Cold.Stored",
				"Stats.EntryCountHot":    "Hot.Count",
				"Stats.EntryCountCold":   "Cold.Count",
			},
		})
}

//#endregion storage

type idxrState struct {
	Name  string
	State string
}

// state simply returns the error state (or lack thereof) of each indexer.
func state() action.Pair {
	return scaffoldlist.NewListAction("display indexer state", "Displays the current error state of each indexer.",
		idxrState{},
		func(fs *pflag.FlagSet) ([]idxrState, error) {
			idxrs, err := connection.Client.GetSystemDescriptions()
			if err != nil {
				return nil, err
			}
			data := make([]idxrState, len(idxrs))
			var i int
			for idxr, stats := range idxrs {
				data[i] = idxrState{
					Name:  idxr,
					State: "OK",
				}
				if stats.Error != "" {
					data[i].State = stats.Error
				}
				i += 1
			}

			return data, nil
		},
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{Use: "state"},
			Pretty: func(fs *pflag.FlagSet) (string, error) {
				idxrs, err := connection.Client.GetSystemDescriptions()
				if err != nil {
					return "", err
				}

				// determine indexer padding
				var longestNameLength int
				for idxr := range idxrs {
					if len(idxr) > longestNameLength {
						longestNameLength = len(idxr)
					}
				}

				// ensure we left pad at least 1 space
				longestNameLength += 1

				// generate output
				var sb strings.Builder
				for idxr, stats := range idxrs {
					// pad indexers to all the same length
					sb.WriteString(strings.Repeat(" ", longestNameLength-len(idxr)) + idxr + " ")

					// colourize state based on whether or not it is in error
					if stats.Error == "" {
						sb.WriteString("is " + stylesheet.Cur.PrimaryText.Render("OK"))
					} else {
						sb.WriteString("is " + stylesheet.Cur.ErrorText.Render("in error!") + "\n")
						sb.WriteString(stylesheet.Cur.ErrorText.Render(stats.Error))
					}
					sb.WriteRune('\n')
				}
				return sb.String()[:sb.Len()-1], nil
			},
		})
}
