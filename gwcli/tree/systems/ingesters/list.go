package ingesters

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/spf13/pflag"
)

// list generates an action for retrieving information about the ingesters.
func list() action.Pair {
	const (
		short string = "review info about all ingesters"
		long  string = "Review general statistics about all ingesters."
	)

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
	}

	return scaffoldlist.NewListAction(short, long, wrappedIngesterStats{},
		func(fs *pflag.FlagSet) ([]wrappedIngesterStats, error) {
			// GetIngesterStats returns data according to each indexer.
			// We extract just the ingester stats sub items.
			// The rest of the stats are inside of the indexer-specific actions // TODO
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
		}, scaffoldlist.Options{})
}
