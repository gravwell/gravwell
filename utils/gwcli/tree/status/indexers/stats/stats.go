package stats

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"gwcli/action"
	"gwcli/clilog"
	"gwcli/connection"
	ft "gwcli/stylesheet/flagtext"
	"gwcli/utilities/scaffold"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	use   string = "stats"
	short string = "review the status of each indexer"
	long  string = "Review the status of each indexer"
)

const (
	defaultPrecision = 2
)

func NewIndexerStatsAction() action.Pair {
	return scaffold.NewBasicAction(use, short, long, []string{},
		func(c *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {

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

			stats, err := connection.Client.GetSystemStats()
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
				res = toJSON(stats)
			case csv:
				//res = toCSV(stats, precisionF)
			case table:
				res = toTbls(stats, precisionF)
			}

			return res, nil

		}, flags)
}

// reformat the results into a single json encoding
func toJSON(ss map[string]types.SysStats) string {

	var res []string
	for k, v := range ss {
		// encode the records as a map
		var m map[string]any = map[string]any{
			k: v.Stats,
		}

		b, err := json.Marshal(m)
		if err != nil {
			clilog.Writer.Errorf("Failed to marshal system stats: %v", err)
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
func toTbls(stats map[string]types.SysStats, precF string) string {
	var sb strings.Builder
	for k, v := range stats {
		sb.WriteString(fmt.Sprintf("%s (error: %v): Uptime: %v | Stats: %#v",
			k, v.Error, v.Stats.Uptime, v.Stats))
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
