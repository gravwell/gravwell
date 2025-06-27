/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package status

import (
	"fmt"
	"net/netip"
	"reflect"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

//#region description

func newDescAction() action.Pair {
	const (
		use   string = "desc"
		short string = "display the description of each indexer"
		long  string = "Display the description of each indexer"
	)

	return scaffold.NewBasicAction(use, short, long, []string{"description"},
		func(c *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
			m, err := connection.Client.GetSystemDescriptions()
			if err != nil {
				return stylesheet.Cur.ErrorText.Render(err.Error()), nil
			}
			// compose descriptions into a single string
			var sb strings.Builder

			for key, inf := range m {
				// key will either be an IP address or "webserver"
				if _, err := netip.ParseAddrPort(key); err == nil {
					sb.WriteString("ingester @ " + key)
				} else {
					sb.WriteString(key)
				}
				// append version to header line
				if inf.SystemVersion != "" {
					sb.WriteString(" | v" + inf.SystemVersion)
				}
				sb.WriteString("\n")

				// attach error
				if inf.Error != "" {
					sb.WriteString(stylesheet.Cur.ErrorText.Render("Error") + ": " + inf.Error + "\n")
				}

				// attach virtualization info
				sb.WriteString(fmt.Sprintf("%v: %s %s\n", stylesheet.Cur.SecondaryText.Render("Virtualization"), inf.VirtSystem, inf.VirtRole))

				// attach CPU info
				cpuM := "unknown"
				if inf.CPUModel != "" {
					cpuM = inf.CPUModel
				}
				sb.WriteString(stylesheet.Cur.SecondaryText.Render("CPU") + ": " + cpuM + "\n")
				sb.WriteString(printIfSet(true, "Clock Speed", inf.CPUMhz, "MHz"))
				sb.WriteString(printIfSet(true, "Thread Count", inf.CPUCount, ""))
				sb.WriteString(printIfSet(true, "Cache Size", inf.CPUCache, "MB"))
				// attach memory
				sb.WriteString(fmt.Sprintf("%v: %vMB\n", stylesheet.Cur.PrimaryText.Render("Total Memory"), inf.TotalMemoryMB))
				sb.WriteString("\n")
			}

			// chip the last newline
			return sb.String()[:sb.Len()-1], nil
		},
		func() pflag.FlagSet {
			return pflag.FlagSet{} // no need for additional flags
		})
}

// helper for the description action.
// Prints the given string (and a newline suffix) if the value is non-empty.
func printIfSet(indent bool, field string, value any, suffix string) string {
	const fieldWidth = 12
	if !reflect.ValueOf(value).IsZero() {
		var s = fmt.Sprintf(stylesheet.Cur.TertiaryText.Width(fieldWidth).Render(field)+": %v%s\n", value, suffix)
		if indent {
			s = stylesheet.Indent + s
		}
		return s
	}
	return ""
}

//#endregion description
