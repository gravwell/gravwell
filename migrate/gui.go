/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/rivo/tview"
)

var (
	app     *tview.Application
	menu    *tview.List
	jobs    *tview.List
	help    *tview.TextView
	logPane *logViewer

	grid *tview.Grid

	jt      *jobTracker
	jobLock sync.Mutex

	helpActive bool
)

type logViewer struct {
	*tview.TextView
}

func (v *logViewer) Close() error {
	return nil
}

func newLogViewer(v *tview.TextView) *logViewer {
	return &logViewer{v}
}

func guiQuit() {
	quit := tview.NewFlex().SetDirection(tview.FlexRow)

	status := tview.NewTextView().SetChangedFunc(func() {
		app.Draw()
	})
	status.SetBorder(true)
	status.SetTitle("Exiting...")
	active := jt.ActiveJobs()
	status.Write([]byte(fmt.Sprintf("Waiting for %d jobs\n", active)))

	quit.AddItem(status, 0, 1, false)
	app.SetRoot(quit, true)

	go func() {
		jt.Shutdown()
		for !jt.JobsDone() {
			time.Sleep(1 * time.Second)
			if curr := jt.ActiveJobs(); curr != active {
				active = curr
				status.Write([]byte(fmt.Sprintf("Waiting for %d jobs\n", active)))
			}
		}
		app.Stop()
	}()
}

func guiMain(doneChan chan bool, st *StateTracker) {
	jt = newJobTracker(cfg)

	defer close(doneChan)

	app = tview.NewApplication()
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlC:
			guiQuit()
			return nil
		case tcell.KeyCtrlY:
			if !helpActive && !menu.HasFocus() {
				app.SetFocus(menu)
			}
		case tcell.KeyCtrlJ:
			if !helpActive && !jobs.HasFocus() {
				app.SetFocus(jobs)
			}
		case tcell.KeyCtrlL:
			if !helpActive && !logPane.HasFocus() {
				app.SetFocus(logPane)
			}
		case tcell.KeyCtrlH:
			toggleHelp()
		case tcell.KeyCtrlP:
			jobClean()
		}
		return event
	})

	menu = tview.NewList()
	menu.SetBorder(true)
	mainMenu()

	jobs = tview.NewList()
	jobs.SetBorder(true).SetTitle("Jobs")

	go jobUpdater()

	logPane = newLogViewer(tview.NewTextView().SetChangedFunc(func() {
		app.Draw()
	}))
	logPane.SetBorder(true).SetTitle("Logs")
	logPane.ScrollToEnd()
	lg.AddWriter(logPane)

	help = tview.NewTextView().
		SetChangedFunc(func() {
			app.Draw()
		})
	help.SetTitle("Help").SetBorder(true)
	help.Write([]byte("Ctrl-Y: Focus main pane    Ctrl-J: Focus jobs pane        Ctrl-L: Focus logs pane\nCtrl-H: Get more help      Ctrl-P: Purge completed jobs   Ctrl-C: Exit"))

	grid = tview.NewGrid().
		SetRows(0, 0, 4).
		SetColumns(0, 0).
		//SetBorders(true).
		AddItem(menu, 0, 0, 2, 1, 0, 0, true).
		AddItem(jobs, 0, 1, 1, 1, 0, 0, false).
		AddItem(logPane, 1, 1, 1, 1, 0, 0, false).
		AddItem(help, 2, 0, 1, 2, 0, 0, false)

	if err := app.SetRoot(grid, true).Run(); err != nil {
		panic(err)
	}
}

func jobClean() {
	jobLock.Lock()
	jobs.Clear()
	for _, j := range jt.GetAllJobs() {
		if j.Done() {
			continue
		}
		jobs.AddItem(j.IdString(), j.LatestUpdate(), 0, nil)
	}
	jobLock.Unlock()
}

func jobUpdater() {
	for {
		jobLock.Lock()
		// Walk each job and extract the job ID from its main text
		for i := 0; i < jobs.GetItemCount(); i++ {
			main, _ := jobs.GetItemText(i)
			var id int
			fmt.Sscanf(main, "Job %d", &id)
			job, err := jt.GetJobById(id)
			if err != nil {
				continue
			}
			//			app.QueueUpdateDraw(func() { jobs.SetItemText(i, job.IdString(), job.LatestUpdate()) })
			jobs.SetItemText(i, job.IdString(), job.LatestUpdate())
		}
		jobLock.Unlock()
		app.Draw()
		time.Sleep(500 * time.Millisecond)
	}
}

func mainMenu() {
	menu.Clear().SetTitle("Main Menu")
	menu.AddItem("Files", "Import files from the disk", 'f', fileMenu)
	menu.AddItem("Splunk", "Import data from Splunk", 's', splunkServerMenu)
	menu.AddItem("Quit", "", 'q', func() {
		guiQuit()
	})
}

func fileMenu() {
	menu.Clear().SetTitle("Select File Config")
	for k, v := range cfg.Files {
		menu.AddItem(k, fmt.Sprintf("%v, filter = %v, tag = %v", v.Base_Directory, v.File_Filter, v.Tag_Name), 0, func() {
			name := k
			fileConfigMenu(name)
		})
	}
	menu.AddItem("Exit", "Previous menu", 'x', mainMenu)
}

func fileConfigMenu(cfgName string) {
	menu.Clear().SetTitle(cfgName)
	menu.AddItem("Start", "Begin migrating files for this config", 's', func() { startFileJob(cfgName) })
	menu.AddItem("Exit", "Previous menu", 'x', mainMenu)
}

func startFileJob(cfgName string) {
	j := jt.StartFileJob(cfgName)
	if j == nil {
		return
	}
	jobLock.Lock()
	defer jobLock.Unlock()
	jobs.AddItem(j.IdString(), "Starting...", 0, nil)
}

func splunkServerMenu() {
	menu.Clear().SetTitle("Select Splunk Server")
	for k, v := range cfg.Splunk {
		name := k
		menu.AddItem(k, fmt.Sprintf("%v", v.Server), 0, func() {
			splunkMenu(name)
		})
	}
	menu.AddItem("Exit", "Previous menu", 'x', mainMenu)
}

func splunkMenu(cfgName string) {
	menu.Clear().SetTitle(cfgName)
	menu.AddItem("Start Migrations", "Migrate some or all data from Splunk", 's', func() { splunkMigrateMenu(cfgName) })
	menu.AddItem("Manage Mappings", "Map index+sourcetype to Gravwell tag", 'm', func() {
		splunkMappingMenu(cfgName)
	})
	menu.AddItem("Exit", "Previous menu", 'x', splunkServerMenu)
}

func splunkMappingMenu(cfgName string) {
	menu.Clear().SetTitle("index+sourcetype→tag mappings")
	menu.AddItem("Scan", "Query Splunk for new index+sourcetype pairs", 's', func() {
		j := jt.StartSourcetypeScanJob(cfgName)
		if j == nil {
			return
		}
		jobLock.Lock()
		jobs.AddItem(j.IdString(), "Starting...", 0, nil)
		jobLock.Unlock()

		go func() {
			for !j.Done() {
				time.Sleep(100 * time.Millisecond)
			}
			splunkMappingMenu(cfgName)
		}()
	})
	menu.AddItem("Write Config", "Write mappings to config file.", 'w', func() { writeMappings(cfgName) })
	menu.AddItem("Exit", "Previous menu", 'x', func() { splunkMenu(cfgName) })
	menu.AddItem("", "", 0, nil)
	maps := splunkTracker.GetStatus(cfgName)
	progresses := maps.GetAll()
	for i := range progresses {
		// we do this so the lambda captures it properly
		x := progresses[i]
		tag := x.Tag
		f := func() {
			setTagMapping(cfgName, x, tag)
		}
		menu.AddItem(fmt.Sprintf("Index: %s, Sourcetype: %s", x.Index, x.Sourcetype), fmt.Sprintf("Tag: %s", tag), 0, f)
	}
}

func writeMappings(cfgName string) {
	var err error
	// Figure out the config we're dealing with
	var c *splunk
	for k, v := range cfg.Splunk {
		if k == cfgName {
			c = v
		}
	}
	if c == nil {
		return
	}

	// Pull back the matching status
	status := splunkTracker.GetStatus(cfgName)

	// Write out a config file containing these mappings
	f, err := os.Create(filepath.Join(*confdLoc, fmt.Sprintf("%s.conf", cfgName)))
	if err != nil {
		// wtf
		lg.Error("Failed to write out sourcetype->tag mappings", log.KVErr(err))
		return
	}
	defer f.Close()

	// Start with the header
	f.Write([]byte(fmt.Sprintf("[Splunk \"%s\"]\n", cfgName)))

	// now do the individual mappings
	for _, m := range status.GetAllFullyMapped() {
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)
		w.Write([]string{m.Index, m.Sourcetype, m.Tag})
		w.Flush()
		out := bytes.TrimSpace(buf.Bytes())
		f.Write([]byte(fmt.Sprintf("Index-Sourcetype-To-Tag=`%s`\n", out)))
	}
}

func setTagMapping(cfgName string, x SplunkToGravwell, tag string) {
	newTag := tag
	startFrom := x.ConsumedUpTo
	save := func() {
		// if we got this far, they typed a valid tag
		x.Tag = newTag
		x.ConsumedUpTo = startFrom
		splunkTracker.Update(cfgName, x)
		app.SetRoot(grid, true).SetFocus(grid)
		splunkMappingMenu(cfgName)
	}
	cancel := func() {
		app.SetRoot(grid, true).SetFocus(grid)
		splunkMappingMenu(cfgName)
	}
	tagCheck := func(tag string, lastChar rune) bool {
		// kill whitespace
		if strings.TrimSpace(tag) != tag {
			return false
		}
		if err := ingest.CheckTag(tag); err != nil {
			return false
		}
		return true
	}
	tagChanged := func(text string) {
		newTag = text
	}
	timestampCheck := func(ts string, lastChar rune) bool {
		return unicode.IsNumber(lastChar)
	}
	startTimestampChanged := func(ts string) {
		if i, err := strconv.Atoi(ts); err == nil {
			startFrom = time.Unix(int64(i), 0)
		}
	}

	// Pop up the form
	form := tview.NewForm().
		AddInputField("Tag", tag, 50, tagCheck, tagChanged).
		AddInputField("(Optional) Unix timestamp to start from", fmt.Sprintf("%d", x.ConsumedUpTo.Unix()), 50, timestampCheck, startTimestampChanged).
		AddButton("Save", save).
		AddButton("Cancel", cancel)

	form.SetBorder(true).SetTitle(fmt.Sprintf("Set tag for index %v, sourcetype %v on config %v", x.Index, x.Sourcetype, cfgName))
	app.SetRoot(form, true)
}

func splunkMigrateMenu(cfgName string) {
	maps := splunkTracker.GetStatus(cfgName)
	progresses := maps.GetAllFullyMapped()
	menu.Clear().SetTitle("Migrate splunk data")
	menu.AddItem("Exit", "Previous menu", 'x', func() { splunkMenu(cfgName) })
	menu.AddItem("Start All", "Launch all jobs (use this with care!)", 0, func() {
		for i := range progresses {
			startMigrate(cfgName, progresses[i])
		}
	})
	menu.AddItem("", "", 0, nil)
	for i := range progresses {
		x := progresses[i]
		tag := x.Tag
		if tag == "" {
			// They have not defined a mapping for this, skip it
			continue
		}
		f := func() {
			startMigrate(cfgName, x)
		}
		timeMsg := fmt.Sprintf("Starting from %v", x.ConsumedUpTo)
		if !x.ConsumeEndTime.IsZero() && x.ConsumeEndTime.Unix() != 0 {
			timeMsg = fmt.Sprintf("From %v to %v", x.ConsumedUpTo, x.ConsumeEndTime)
		}
		menu.AddItem(fmt.Sprintf("%s, %s -> %s", x.Index, x.Sourcetype, x.Tag), timeMsg, 0, f)
	}
}

func startMigrate(cfgName string, progress SplunkToGravwell) {
	j := jt.StartSplunkJob(cfgName, progress)
	if j == nil {
		return
	}
	jobLock.Lock()
	defer jobLock.Unlock()
	jobs.AddItem(j.IdString(), "Starting...", 0, nil)
}

func toggleHelp() {
	if !helpActive {
		bigHelp := tview.NewTextView().SetChangedFunc(func() {
			app.Draw()
		})
		bigHelp.SetTitle("Help").SetBorder(true)
		bigHelp.Write([]byte("This is the help document."))
		app.SetRoot(bigHelp, true)
	} else {
		app.SetRoot(grid, true)
	}
	helpActive = !helpActive
}
