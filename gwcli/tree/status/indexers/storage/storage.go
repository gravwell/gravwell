/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package storage

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	use   string = "storage"
	short string = "review storage information for each indexer"
	long  string = "Review storage information for each indexer"
)

const (
	defaultPrecision = 2
)

func NewIndexerStorageAction() action.Pair {
	return scaffold.NewBasicAction(use, short, long, []string{},
		func(_ *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
			// check for mutually exclusive flags
			var json, csv, table bool
			var set int
			var err error
			if json, err = fs.GetBool(ft.Name.JSON); err != nil {
				clilog.LogFlagFailedGet(ft.Name.JSON, err)
			} else if json {
				set += 1
			}
			if csv, err = fs.GetBool(ft.Name.CSV); err != nil {
				clilog.LogFlagFailedGet(ft.Name.CSV, err)
			} else if csv {
				set += 1
			}
			if table, err = fs.GetBool(ft.Name.Table); err != nil {
				clilog.LogFlagFailedGet(ft.Name.Table, err)
			} else if table {
				set += 1
			}
			if set > 1 { // too many were set
				return "[json csv table] are mutually exclusive", nil
			} else if set == 0 { // none were set
				table = true
			}

			ss, err := connection.Client.GetStorageStats()
			if err != nil {
				return err.Error(), nil
			}

			var precisionF string // precision format
			// pull precision from flag
			if precision, err := fs.GetUint8("precision"); err != nil {
				clilog.LogFlagFailedGet("precision", err)
			} else {
				precisionF = "%." + strconv.FormatUint(uint64(precision), 10) + "f"
			}

			// select format
			var res string
			switch {
			case json:
				res = toJSON(ss, precisionF)
			case csv:
				res = toCSV(ss, precisionF)
			case table:
				res = toTbls(ss, precisionF)
			}

			return res, nil

		},
		flags)
}

// reformat the results into a single json encoding
func toJSON(ss map[string]types.StorageStats, precF string) string {
	type indexer struct {
		Kind         string
		EntryCount   uint64
		DataIngested string
		DataStored   string
	}

	var res []string
	for k, v := range ss {
		// encode the records as a map
		var m = map[string]any{
			k: []indexer{
				{
					Kind:         "hot",
					EntryCount:   v.EntryCountHot,
					DataIngested: gb(v.DataIngestedHot, precF),
					DataStored:   gb(v.DataStoredHot, precF),
				},
				{
					Kind:         "cold",
					EntryCount:   v.EntryCountCold,
					DataIngested: gb(v.DataIngestedCold, precF),
					DataStored:   gb(v.DataStoredCold, precF),
				},
			},
		}

		b, err := json.Marshal(m)
		if err != nil {
			clilog.Writer.Errorf("Failed to marshal storage stats: %v", err)
		}
		res = append(res, (string(b)))
	}

	return strings.Join(res, "\n")

}

// reformat the results into a single csv encoding
func toCSV(ss map[string]types.StorageStats, precF string) string {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	w.Write([]string{"indexer", "kind", "entries", "ingested", "stored"})
	for k, v := range ss {
		w.Write([]string{k, "hot", strconv.FormatUint(v.EntryCountHot, 10), gb(v.DataIngestedHot, precF), gb(v.DataStoredHot, precF)})
		w.Write([]string{k, "cold", strconv.FormatUint(v.EntryCountCold, 10), gb(v.DataIngestedCold, precF), gb(v.DataStoredCold, precF)})
	}

	w.Flush()

	return buf.String()
}

// reformat the results into one table per index
func toTbls(ss map[string]types.StorageStats, precF string) string {
	var sb strings.Builder
	for k, v := range ss {
		sb.WriteString(fmt.Sprintf("%v: %v -> %v\n", k, v.CoverageStart, v.CoverageEnd))
		tbl := stylesheet.Table()
		tbl.Headers("kind", "entries", "ingested", "stored")

		tbl.Row(
			"hot",
			strconv.FormatUint(v.EntryCountHot, 10),
			gb(v.DataIngestedHot, precF),
			gb(v.DataStoredHot, precF),
		)
		tbl.Row(
			"cold",
			strconv.FormatUint(v.EntryCountCold, 10),
			gb(v.DataIngestedCold, precF),
			gb(v.DataStoredCold, precF),
		)
		sb.WriteString(tbl.Render() + "\n")
	}

	return sb.String()
}

// Add additional flags for data representation, a la scaffoldlist.
func flags() pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.Uint8("precision", defaultPrecision,
		"decimal precision when displaying floating point numbers")
	fs.Bool(ft.Name.CSV, false, ft.Usage.CSV)
	fs.Bool(ft.Name.JSON, false, ft.Usage.JSON)
	fs.Bool(ft.Name.Table, false, ft.Usage.Table)
	return fs
}

// format bytes as uint64 to a string, as gigabytes
func gb(bytes uint64, precF string) string {
	return fmt.Sprintf(precF+"GB", ((float64(bytes)/1024)/1024)/1024)
}
