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

func NewStatusStorageAction() action.Pair {
	return scaffold.NewBasicAction(use, short, long, []string{},
		func(*cobra.Command, *pflag.FlagSet) (string, tea.Cmd) {
			ss, err := connection.Client.GetStorageStats()
			if err != nil {
				return err.Error(), nil
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
					fmt.Sprintf("%.2fGB", ingestedHotMB/1024),
					fmt.Sprintf("%.2fGB", storedHotMB/1024),
				)
				tbl.Row(
					"cold",
					strconv.FormatUint(v.EntryCountCold, 10),
					fmt.Sprintf("%.2fGB", ingestedColdMB/1024),
					fmt.Sprintf("%.2fGB", storedColdMB/1024),
				)
				sb.WriteString(tbl.Render() + "\n")
			}

			return sb.String(), nil
		},
		nil)
}
