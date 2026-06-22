/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package ingesters contains actions for fetching information about the state of the ingesters.
package ingesters

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewIngestersNav() *cobra.Command {
	const (
		use   string = "ingesters"
		short string = "view ingesters status"
		long  string = "Review information for all ingesters or get detailed information about a specific ingester."
	)

	return treeutils.GenerateNav(use, short, long, []string{},
		[]*cobra.Command{},
		[]action.Pair{
			listAction(),
			get(),
		})
}

// listAction generates an action for retrieving information about the ingesters.
func listAction() action.Pair {
	type wrappedIngesterStats struct {
		Indexer       string
		Hostname      string
		RemoteAddress string
		Count         uint64
		Size          uint64
		Uptime        string
		Tags          []string
		Name          string
		Version       string
		UUID          string
	}

	return scaffoldlist.NewListAction("review info about all ingesters", "Review general statistics about all ingesters.", wrappedIngesterStats{},
		func(fs *pflag.FlagSet, _ scaffoldlist.DataParameters) ([]wrappedIngesterStats, error) {
			// GetIngesterStats returns data according to each indexer.
			// We extract just the ingester stats sub items.
			// The rest of the stats are inside of the indexer-specific actions.
			ss, err := connection.Client.GetIngesterStats()
			if err != nil {
				return nil, err
			}
			// transform the data
			var wrap = make([]wrappedIngesterStats, 0)
			for idxr, stats := range ss { // walk each indexer
				for _, ingstr := range stats.Ingesters { // walk each ingester
					wrap = append(wrap, wrappedIngesterStats{
						Indexer:       idxr,
						Hostname:      ingstr.State.Hostname,
						RemoteAddress: ingstr.RemoteAddress,
						Count:         ingstr.Count,
						Size:          ingstr.Size,
						Uptime:        ingstr.Uptime.String(),
						Tags:          ingstr.Tags,
						Name:          ingstr.Name,
						Version:       ingstr.Version,
						UUID:          ingstr.UUID,
					})
				}
			}
			return wrap, nil
		}, nil, scaffoldlist.Options{Omit: scaffold.OmitFlags{Everything: true}})
}
