/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package storage

import (
	"fmt"
	"gwcli/action"
	"gwcli/clilog"
	"gwcli/connection"
	"gwcli/stylesheet"
	"gwcli/utilities/scaffold"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	use   string = "storage"
	short string = ""
	long  string = "Review storage information for each indexer"
)

const (
	defaultPrecision = 2
)

func NewStatusStorageAction() action.Pair {
	return scaffold.NewBasicAction(use, short, long, []string{},
		func(_ *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
			ss, err := connection.Client.GetStorageStats()
			if err != nil {
				return err.Error(), nil
			}

			// pull precision from flag
			var precisionF string
			if precision, err := fs.GetUint8("precision"); err != nil {
				clilog.LogFlagFailedGet("precision", err)
			} else {
				precisionF = "%." + strconv.FormatUint(uint64(precision), 10) + "f"
			}

			var sb strings.Builder
			for k, v := range ss {
				sb.WriteString(fmt.Sprintf("%v: %v -> %v\n", k, v.CoverageStart, v.CoverageEnd))
				tbl := stylesheet.Table()
				tbl.Headers("kind", "entries", "ingested", "stored")

				// human-represent the data
				ingestedHotMB := (float64(v.DataIngestedHot) / 1024) / 1024
				storedHotMB := (float64(v.DataStoredHot) / 1024) / 1024

				ingestedColdMB := (float64(v.DataIngestedCold) / 1024) / 1024
				storedColdMB := (float64(v.DataStoredCold) / 1024) / 1024

				tbl.Row(
					"hot",
					strconv.FormatUint(v.EntryCountHot, 10),
					fmt.Sprintf(precisionF+"GB", ingestedHotMB/1024),
					fmt.Sprintf(precisionF+"GB", storedHotMB/1024),
				)
				tbl.Row(
					"cold",
					strconv.FormatUint(v.EntryCountCold, 10),
					fmt.Sprintf(precisionF+"GB", ingestedColdMB/1024),
					fmt.Sprintf(precisionF+"GB", storedColdMB/1024),
				)
				sb.WriteString(tbl.Render() + "\n")
			}

			return sb.String(), nil
		},
		flags)
}

// Add additional flags for data representation, a la scaffoldlist.
func flags() pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.Uint8("precision", defaultPrecision,
		"decimal precision when displaying floating point numbers")
	return fs
}
