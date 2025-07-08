/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package systemshealth

import (
	"fmt"
	"net/netip"
	"reflect"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/spf13/cobra"
)

// The hardware action fetches and averages system statistics.
// Under the hood, it gathers all the required information (via a couple of API calls) before piecing it together in the main thread.
// NOTE(rlandau): most of the info contained in here is available via ingesters get,
// so no need to make it support all the list semantics (hence the basic action).
func newHardwareAction() action.Pair {
	const (
		use   string = "hardware"
		short string = "see hardware information and statistics"
		long  string = "Preformatted information about the hardware platforms powering your indexers and ingesters.\n" +
			"This action is not particularly script-friendly, but most of this information is available in JSON/CSV via the indexer and ingester actions."
	)
	return scaffold.NewBasicAction(use, short, long, []string{"hw"},
		func(c *cobra.Command) (string, tea.Cmd) {
			var sb strings.Builder

			// attach descriptions
			if err := attachDescriptions(&sb); err != nil {
				clilog.Writer.Errorf("%v", err)
				sb.WriteString(stylesheet.Cur.ErrorText.Render(err.Error() + "\n"))
			}

			// attach stats
			overview, err := gatherStats()
			if err != nil {
				clilog.Writer.Errorf("%v", err)
				sb.WriteString(stylesheet.Cur.ErrorText.Render(err.Error() + "\n"))
			}
			sb.WriteString(stylesheet.Cur.PrimaryText.Bold(true).Render("System Stats (Averages)") + "\n") // TODO should we bold this?

			{ // reformat the floats as strings and colorize them
				// we need to pre-format the strings, otherwise Go will get confused counting the ASCII escapes.
				cuField := fmt.Sprintf("%-13s", "CPU Usage:")
				cu := stylesheet.Cur.SecondaryText.Render(fmt.Sprintf("%5.2f", overview.CPUAvgUsage))
				muField := fmt.Sprintf("%-13s", "Memory Usage:")
				mu := stylesheet.Cur.SecondaryText.Render(fmt.Sprintf("%5.2f", overview.MemAvgUsage))
				sb.WriteString(stylesheet.Cur.FieldText.Render(cuField) + " " + cu + "\n")
				sb.WriteString(stylesheet.Cur.FieldText.Render(muField) + " " + mu + "\n")
			}
			{ // now for disks
				headerField := stylesheet.Cur.PrimaryText.Bold(true).Render(fmt.Sprintf("Disks[%d]", overview.Disks.Count))
				totalField := fmt.Sprintf("%12s", "Total Space:")
				usedField := fmt.Sprintf("%12s", "Space Used:")

				// convert accumulations to GB
				totalGB := ((float64(overview.Disks.Total) / 1024) / 1024) / 1024
				usedGB := ((float64(overview.Disks.Used) / 1024) / 1024) / 1024

				sb.WriteString(
					fmt.Sprintf("%s\n"+stylesheet.Indent+"%s %.2fGB\n"+stylesheet.Indent+"%s %.2fGB\n",
						headerField,
						stylesheet.Cur.FieldText.Render(totalField), totalGB,
						stylesheet.Cur.FieldText.Render(usedField), usedGB,
					),
				)
			}

			return sb.String(), nil

			// chip the last newline // TODO is this still necessary
			//return sb.String()[:sb.Len()-1], nil
		},
		nil)
}

func attachDescriptions(sb *strings.Builder) error {
	m, err := connection.Client.GetSystemDescriptions()
	if err != nil {
		return err
	}

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
		// append virtualization info to the header line
		sb.WriteString(fmt.Sprintf(" | %s (%s)", inf.VirtSystem, inf.VirtRole))

		sb.WriteString("\n")

		// attach error
		if inf.Error != "" {
			sb.WriteString(stylesheet.Cur.ErrorText.Render("Error") + ": " + inf.Error + "\n")
		}

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

	return nil
}

type indexer struct {
	title string // <ip>:<port>, typically
	disks types.DiskStats
}

// ovrvw holds the collected averages and totals calculated by gatherStats().
type ovrvw struct {
	CPUAvgUsage float64
	MemAvgUsage float64
	Disks       struct {
		Count            uint
		Total            uint64
		Used             uint64
		AvgReadsPerSecB  float64 // TODO where do these data come from?
		AvgWritesPerSecB float64 // TODO where do these data come from?
	}
	AvgUp   float64
	AvgDown float64
}

// TODO annotate
func gatherStats() (fullInfo []indexer, _ ovrvw, _ error) {
	var o = ovrvw{
		CPUAvgUsage: -1,
		MemAvgUsage: -1,
		Disks: struct {
			Count            uint
			Total            uint64
			Used             uint64
			AvgReadsPerSecB  float64
			AvgWritesPerSecB float64
		}{0, 0, 0, -1, -1},
	}

	stats, err := connection.Client.GetSystemStats()
	if err != nil {
		return o, err
	}
	i := 0

	var cpuSamples, memSamples uint

	for idxr, stat := range stats {
		full := indexer{title: idxr} // TODO insert full into an array
		// check for an error
		if stat.Error != "" {
			clilog.Writer.Warnf("failed to get statistics for indexer '%s': %v", idxr, stat.Error)
			continue
		}
		// accumulate for averages
		o.CPUAvgUsage += stat.Stats.CPUUsage
		cpuSamples += 1
		o.MemAvgUsage += stat.Stats.MemoryUsedPercent
		memSamples += 1

		for _, disk := range stat.Stats.Disks {
			// attach all info to indexer hw
			full.disks = disk
			// accumulate special stats
			o.Disks.Count += 1
			o.Disks.Total += disk.Total
			o.Disks.Used += disk.Used

		}

		// TODO save off other data

		i += 1
	}

	if memSamples != cpuSamples { // sanity check: these should never be out of sync
		clilog.Writer.Errorf("cpu sample count (%v) and memory sample count (%v) are out of sync", cpuSamples, memSamples)
	}

	if cpuSamples > 0 {
		o.CPUAvgUsage /= float64(cpuSamples)
		o.MemAvgUsage /= float64(cpuSamples)
	}

	return o, nil
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
