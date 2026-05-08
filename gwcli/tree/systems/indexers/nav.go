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
	"github.com/google/uuid"
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
			get(),
			list(),
			newCalendarAction(),
			wells(),
		})
}

// Wells just returns the list of all wells.
func wells() action.Pair {
	type wd struct {
		Indexer struct {
			UUID uuid.UUID
			Name string
		}
		ID          string // unique identifier constructed from the indexer UUID and the well name
		Name        string
		Tags        []string
		Shards      []types.ShardInfo
		Accelerator string `json:",omitempty"`
		Engine      string `json:",omitempty"`
		Path        string `json:",omitempty"` //hot storage location
		ColdPath    string `json:",omitempty"` //cold storage location
	}

	return scaffoldlist.NewListAction("get a list of all wells", "returns the indexer each well is associated to and the well's full id", wd{}, func(fs *pflag.FlagSet) ([]wd, error) {
		wells, err := connection.Client.WellData()
		if err != nil {
			return nil, err
		}
		toRet := make([]wd, 0)
		for idxrName, iwd := range wells {
			for _, well := range iwd.Wells {
				toRet = append(toRet, wd{
					Indexer: struct {
						UUID uuid.UUID
						Name string
					}{
						iwd.UUID,
						idxrName,
					},
					ID:          well.ID,
					Name:        well.Name,
					Tags:        well.Tags,
					Shards:      well.Shards,
					Accelerator: well.Accelerator,
					Engine:      well.Engine,
					Path:        well.Path,
					ColdPath:    well.ColdPath,
				})
			}

		}
		return toRet, nil
	}, scaffoldlist.Options{
		Use:            "wells",
		Aliases:        []string{"well"},
		DefaultColumns: []string{"Indexer.UUID", "Indexer.Name", "ID", "Name", "Tags", "Accelerator", "Engine", "Path", "ColdPath"},
	})
}
