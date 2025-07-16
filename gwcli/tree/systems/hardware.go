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
	"reflect"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/spf13/cobra"
)

const (
	fieldWidth string = "20"
)

// styles, set in init()
var (
	h1sty lipgloss.Style
	h2sty lipgloss.Style
	h3sty lipgloss.Style
)

func init() {
	// set local styles based on stylesheet's state
	h1sty = stylesheet.Cur.TertiaryText.Bold(true)
	h2sty = stylesheet.Cur.SecondaryText.Bold(true)
	h3sty = stylesheet.Cur.TertiaryText.Bold(true)

}

// The hardware action fetches and averages system statistics.
// Under the hood, it gathers all the required information (via a couple of API calls) before piecing it together in the main thread.
// NOTE(rlandau): most of the info contained in here is available via ingesters get,
// so no need to make it support all the list semantics (hence the basic action).
func newHardwareAction() action.Pair {
	const (
		use   string = "hardware"
		short string = "see hardware information and statistics"
		long  string = "Preformatted information about the hardware platforms powering your indexers and ingesters.\n" +
			"This action is intended for human consumption; most of this information is available in JSON/CSV via the indexer and ingester actions if you need better script support."
	)
	return scaffold.NewBasicAction(use, short, long, []string{"hw"},
		func(c *cobra.Command) (string, tea.Cmd) {
			var sb strings.Builder

			var (
				o = ovrvw{
					CPUAvgUsage: -1,
					MemAvgUsage: -1,
					Disks: struct {
						DiskCount        uint
						Total            uint64
						Used             uint64
						IOCount          uint
						AvgReadsPerSecB  float64
						AvgWritesPerSecB float64
					}{0, 0, 0, 0, -1, -1},
				}
			)

			metrics, err := connection.Client.GetSystemStats()
			if err != nil {
				clilog.Writer.Errorf("%v", err.Error())
				return stylesheet.Cur.ErrorText.Render(err.Error()), nil
			}
			{ // collect averages and accumulations
				i := 0

				var cpuSamples, memSamples uint

				for idxr, stat := range metrics {
					if idxr == "webserver" {
						// the GUI skips the webserver, as do we
						continue
					}
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

					o.Disks.DiskCount += uint(len(stat.Stats.Disks))
					for _, disk := range stat.Stats.Disks { // accumulate special stats
						o.Disks.Total += disk.Total
						o.Disks.Used += disk.Used
					}
					o.Disks.IOCount += uint(len(stat.Stats.IO))
					for _, io := range stat.Stats.IO {
						o.Disks.AvgReadsPerSecB += float64(io.Read)
						o.Disks.AvgWritesPerSecB += float64(io.Write)
					}

					i += 1
				}

				if memSamples != cpuSamples { // sanity check: these should never be out of sync
					clilog.Writer.Errorf("cpu sample count (%v) and memory sample count (%v) are out of sync", cpuSamples, memSamples)
				}

				if cpuSamples > 0 {
					o.CPUAvgUsage /= float64(cpuSamples)
					o.MemAvgUsage /= float64(cpuSamples)
				}
				if o.Disks.IOCount > 0 {
					o.Disks.AvgReadsPerSecB /= float64(o.Disks.IOCount)
					o.Disks.AvgWritesPerSecB /= float64(o.Disks.IOCount)
				}
			}

			hw, err := connection.Client.GetSystemDescriptions()
			if err != nil {
				clilog.Writer.Errorf("%v", err.Error())
				return stylesheet.Cur.ErrorText.Render(err.Error()), nil
			}

			s, llw := constructIndexers(hw, metrics)
			sb.WriteString(s)

			sb.WriteString(constructOverview(o, llw))
			return sb.String(), nil
		},
		nil)
}

// writeOverview attach the stat averages and cumulated disk data to the string builder
func constructOverview(o ovrvw, width int) string {
	var avgs, disks, disksTitle string

	{ // reformat the floats as strings and colorize them
		// we need to pre-format the strings, otherwise Go will get confused counting the ASCII escapes.
		cu := fmt.Sprintf("%6.2f", o.CPUAvgUsage)
		mu := fmt.Sprintf("%6.2f", o.MemAvgUsage)
		avgs = stylesheet.Cur.FieldText.Render(field("Avg CPU Usage")) + " " + cu + "%\n"
		avgs += stylesheet.Cur.FieldText.Render(field("Avg Memory Usage")) + " " + mu + "%"
	}
	{ // now for disks
		disksTitle = " " + stylesheet.Cur.SecondaryText.Bold(true).Render(fmt.Sprintf("Disks[%d]", o.Disks.DiskCount)) + " "
		totalField := field("Total Space")
		usedField := field("Space Used")
		avgReadField := field("Avg Reads/sec")
		avgWriteField := field("Avg Writes/sec")

		// convert accumulations to GB
		totalGB := fmt.Sprintf("%8.2f", ((float64(o.Disks.Total)/1024)/1024)/1024)
		usedGB := fmt.Sprintf("%8.2f", ((float64(o.Disks.Used)/1024)/1024)/1024)
		// convert read/write to KB (and max to avoid negatives)
		readMB := fmt.Sprintf("%8.2f", max(o.Disks.AvgReadsPerSecB/1024/1024, 0))
		writeMB := fmt.Sprintf("%8.2f", max(o.Disks.AvgWritesPerSecB/1024/1024, 0))

		disks = fmt.Sprintf(
			"%s %sGB\n"+
				"%s %sGB\n"+
				"%s %sMB\n"+
				"%s %sMB",
			totalField, totalGB,
			usedField, usedGB,
			avgReadField, readMB,
			avgWriteField, writeMB,
		)
	}

	if s, err := stylesheet.SegmentedBorder(stylesheet.Cur.ComposableSty.ComplimentaryBorder.BorderForeground(stylesheet.Cur.PrimaryText.GetForeground()), width, struct {
		StylizedTitle string
		Contents      string
	}{h1sty.Render(" Overview "), avgs}, struct {
		StylizedTitle string
		Contents      string
	}{disksTitle, disks}); err != nil {
		clilog.Writer.Warnf("failed to generate overview: %v", err)
		return "failed to generate overview"
	} else {
		return s
	}

}

func constructIndexers(desc map[string]types.SysInfo, sys map[string]types.SysStats) (_ string, longestLineWidth int) {
	var ( // length of the longest line
		writeString = func(sb *strings.Builder, stylizedStr string) { // test and (if new longest) record length prior to writing to the builder
			longestLineWidth = max(lipgloss.Width(stylizedStr), longestLineWidth)
			sb.WriteString(stylizedStr)
		}
	)

	var toRet strings.Builder

	for idxr, stat := range sys {
		if idxr == "webserver" {
			// the GUI skips the webserver, as do we
			continue
		}

		// 1: empty section for just the indexer name + version
		// 2: health section
		// 3: disks section
		// 4: specs section
		// if stat.Err != "", health and disks are consolidated into a single section
		sections := []struct {
			StylizedTitle string
			Contents      string
		}{
			{
				StylizedTitle: h1sty.Render(idxr),
			},
			/*{
							StylizedTitle: h2sty.Render("Health"),
						},
						{
							StylizedTitle: h2sty.Render("Disks"),
						},
						{
			StylizedTitle: h2sty.Render("Specifications"),
						},*/
		}

		longestLineWidth = lipgloss.Width(sections[0].StylizedTitle)

		// collect health and disk stats
		if stat.Error != "" {
			clilog.Writer.Warnf("failed to stat indexer %v: %v", idxr, stat.Error)
			// apply a wrap to the error, as we have no scale for length
			e := stylesheet.Cur.ErrorText.Width(40).Render(stat.Error)
			sections = append(sections, struct {
				StylizedTitle string
				Contents      string
			}{h2sty.Render("Health & Disks"), e})
		} else {
			var sb strings.Builder
			{ // health section
				sctn := struct {
					StylizedTitle string
					Contents      string
				}{
					StylizedTitle: h2sty.Render(" Health") + " (" + h2sty.Render(stat.Stats.BuildInfo.CanonicalVersion.String()) + ") ",
				}
				// generate content
				uptimeField := stylesheet.Cur.FieldText.Render(fmt.Sprintf("%"+fieldWidth+"s", "Uptime:"))
				writeString(&sb, uptimeField+" "+(time.Duration(stat.Stats.Uptime)*time.Second).String()+"\n")
				netField := stylesheet.Cur.FieldText.Render(fmt.Sprintf("%"+fieldWidth+"v", "Up/Down:"))
				netUpKB := float64(stat.Stats.Net.Up) / 1024
				netDownKB := float64(stat.Stats.Net.Down) / 1024
				writeString(&sb, fmt.Sprintf("%s %.2fKB/%.2fKB\n", netField, netUpKB, netDownKB))
				var readMB, writeMB float64
				for _, b := range stat.Stats.IO {
					readMB += float64(b.Read)
					writeMB += float64(b.Write)
				}
				readMB = readMB / 1024 / 1024
				writeMB = writeMB / 1024 / 1024
				writeString(&sb, fmt.Sprintf("%s %.2fKB/%.2fKB", field("Read/Write"), readMB, writeMB))
				// write content
				sctn.Contents = sb.String()
				sections = append(sections, sctn)
			}
			sb.Reset()
			{ // disk section
				sctn := struct {
					StylizedTitle string
					Contents      string
				}{
					StylizedTitle: h2sty.Render(fmt.Sprintf(" Disk[%d] ", len(stat.Stats.Disks))),
				}
				// generate content
				for _, d := range stat.Stats.Disks {
					usedGB := ((float64(d.Used) / 1024) / 1024) / 1024
					totalGB := ((float64(d.Total) / 1024) / 1024) / 1024

					writeString(&sb, fmt.Sprintf("%s\n"+stylesheet.Indent+"'%s' mounted at %s\n"+
						stylesheet.Indent+stylesheet.Indent+"%.2fGB used of %.2fGB total",
						stylesheet.Cur.TertiaryText.Render(d.ID),
						d.Partition, d.Mount,
						usedGB, totalGB,
					))
				}
				// write content
				sctn.Contents = sb.String()
				sections = append(sections, sctn)
			}
		}
		s, err := stylesheet.SegmentedBorder(stylesheet.Cur.ComposableSty.ComplimentaryBorder.BorderForeground(stylesheet.Cur.PrimaryText.GetForeground()), longestLineWidth, sections...)
		if err != nil {
			// TODO
		} else {
			toRet.WriteString(s)
		}
		/*
			hw, ok := desc[idxr]
			if !ok {
				continue
			} else if hw.Error != "" {
				clilog.Writer.Warnf("failed to stat indexer hardware %v: %v", idxr, hw.Error)
				sb.WriteString(stylesheet.Cur.ErrorText.Render(hw.Error) + "\n")
			} else {
				// specs section
				sb.WriteString()
				// attach virtualization info
				sb.WriteString(" (" + h3sty.Render(fmt.Sprintf("%v[%v]", hw.VirtSystem, hw.VirtRole)) + ")\n")
				// attach hardware info
				fmt.Fprintf(sb,
					"%s %s\n"+
						"%s %s\n"+
						"%s %d\n"+
						"%s %sMHz\n"+
						"%s %sKB per CPU\n"+
						"%s %dMB\n", // I believe this is L2/core and L3/thread
					field("System Version"), hw.SystemVersion,
					field("CPU Model"), hw.CPUModel,
					field("CPU Count"), hw.CPUCount,
					field("CPU Clock Speed"), hw.CPUMhz,
					field("CPU Cache Size"), hw.CPUCache,
					field("Total Memory"), hw.TotalMemoryMB,
				)
			}*/
	}

	return toRet.String(), longestLineWidth
}

// styles the given text as a field by colorizing it and appending a colon.
func field(fieldText string) string {
	return stylesheet.Cur.FieldText.Render(fmt.Sprintf("%"+fieldWidth+"s", ""+fieldText+":"))
}

/*func attachDescriptions(sb *strings.Builder) error {

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
}*/

// ovrvw holds the collected averages and totals calculated by gatherStats().
type ovrvw struct {
	CPUAvgUsage float64
	MemAvgUsage float64
	Disks       struct {
		DiskCount        uint
		Total            uint64
		Used             uint64
		IOCount          uint // # of items in types.SysStats.Stats.IO
		AvgReadsPerSecB  float64
		AvgWritesPerSecB float64
	}
	AvgUp   float64
	AvgDown float64
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
