/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package datascope implements a tabbed, scrolling viewport with a paginator built into the results view.

It displays and manages results from a search.
As the user pages through, the viewport automatically updates with the contents of the new page.
The first tab contains the actual results, while the following tabs provide controls for
downloading the results and scheduling the query

Like busywait, this can be invoked by Cobra as a standalone tea.Model or as a child of an action
spawned by Mother.
*/
package datascope

import (
	"errors"
	"os"
	"time"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/killer"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	grav "github.com/gravwell/gravwell/v4/client"
)

// Meant to be called as a goroutine that provides a heartbeat for the search id.
// Pings every search.Interval()/2.
// If the given done channel is closed (or any value is received on it), the goroutine will return and pings will stop.
func keepAlive(search *grav.Search, done chan bool) {
	var mysid = search.ID
	var pingFreq = search.Interval() / 2 // ping twice per pre-set interval
	for {
		// check if we are being signalled to be done
		select {
		case <-done:
			clilog.Writer.Debugf("keepAlive found done signal")
			return
		default:
		}
		if err := search.Ping(); err != nil {
			clilog.Writer.Warnf("keepAlive: ping failed: %v", err)
			break
		}
		clilog.Writer.Debugf("pinged search %v", mysid)
		time.Sleep(pingFreq)
	}
}

type DataScope struct {
	motherRunning bool // without Mother's support, we need to handle killkeys and death alone

	Done chan bool

	rawHeight int // usable height, as reported by the tty
	rawWidth  int // usabe width, as reported by the tty

	tabs      []tab // TODO junk tab array?
	showTabs  bool
	activeTab uint

	search *grav.Search // the search being displayed

	download downloadTab
	schedule scheduleTab

	tableMode bool
	table     tableTab
	results   resultsTab
}

type DataScopeOption func(*DataScope) error

// NewDataScope returns a new DataScope instance based on the given data array.
// If mother is running, this subroutine will launch her into the alt screen buffer and query the
// terminal for its size.
// Table mode indicates if the results should be displayed in a tabular method, replacing the normal
// display method/struct.
func NewDataScope(data []string, motherRunning bool,
	search *grav.Search, table bool, opt ...DataScopeOption,
) (
	DataScope, tea.Cmd, error,
) {
	// sanity check arguments
	if search == nil {
		return DataScope{}, nil, errors.New("search cannot be nil")
	}
	if len(data) == 0 {
		return DataScope{}, nil, errors.New("no data to display")
	}

	s := DataScope{
		Done:          make(chan bool),
		tableMode:     table,
		motherRunning: motherRunning,
		download:      initDownloadTab("", false, false, false),
		schedule:      initScheduleTab("", "", ""),
	}

	// set up tabs
	s.tabs = s.generateTabs()
	s.activeTab = results
	s.showTabs = true

	// save search
	s.search = search

	// set up normal results or table, depending on mode
	if s.tableMode {
		// replace tabs[results] subroutines
		s.tabs[results].name = "table"
		s.tabs[results].updateFunc = updateTable
		s.tabs[results].viewFunc = viewTable
		s.table = initTableTab(data)
	} else {
		s.results = initResultsTab(data)
	}
	// launch heartbeat gorotuine
	go keepAlive(search, s.Done)

	// apply options
	for _, o := range opt {
		if o == nil {
			continue
		}

		if err := o(&s); err != nil {
			return DataScope{}, nil, err
		}
	}

	// mother does not start in alt screen, and thus requires manual measurements
	if motherRunning {
		return s, tea.Sequence(tea.EnterAltScreen, func() tea.Msg {
			w, h, err := term.GetSize(os.Stdin.Fd())
			if err != nil {
				clilog.Writer.Errorf("Failed to fetch terminal size: %v", err)
			}
			return tea.WindowSizeMsg{Width: w, Height: h}
		}), nil
	}

	return s, nil, nil
}

//#region constructor options

// WithAutoDownload prep-populates the download tab's values and, if able, automatically download the results in the
// given format.
func WithAutoDownload(outfn string, append, json, csv bool) DataScopeOption {
	return func(ds *DataScope) error {
		if json && csv {
			return errors.New("output format cannot be both JSON and CSV")
		}

		ds.download = initDownloadTab(outfn, append, json, csv)
		if outfn != "" {
			res, success := ds.dl(outfn)
			ds.download.resultString = res
			if !success {
				clilog.Writer.Error(res)
			} else {
				clilog.Writer.Info(res)
			}
		}
		return nil
	}
}

// WithSchedule pre-populates the schedule tab's values and, if able, automatically schedule the query.
func WithSchedule(cronfreq, name, desc string) DataScopeOption {
	return func(ds *DataScope) error {
		ds.schedule = initScheduleTab(cronfreq, name, desc)
		if cronfreq == "" && name == "" && desc == "" {
			return nil
		}
		ds.sch()
		return nil
	}
}

//#endregion

// Init is unused in Datascope.
func (s DataScope) Init() tea.Cmd {
	return nil
}

// Update handles incoming keys, typically passing them to the update function for the current tab.
func (s DataScope) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// mother takes care of kill keys if she is running
	if !s.motherRunning {
		if kill := killer.CheckKillKeys(msg); kill != killer.None {
			clilog.Writer.Infof("Self-handled kill key, with kill type %v", kill)
			return s, tea.Batch(tea.Quit, tea.ExitAltScreen)
		}
	}

	switch msg := msg.(type) {
	case tea.KeyMsg: // tab-agnostic keys
		switch {
		case key.Matches(msg, keys.showTabs):
			s.showTabs = !s.showTabs
			// recalculate height and update display
			s.recalculateWindowMargins(s.rawWidth, s.rawHeight)
			return s, textinput.Blink
		case key.Matches(msg, keys.cycleTabs):
			s.activeTab += 1
			if s.activeTab >= uint(len(s.tabs)) {
				s.activeTab = 0
			}
			return s, textinput.Blink

		case key.Matches(msg, keys.reverseCycleTabs):
			if s.activeTab == 0 {
				s.activeTab = uint(len(s.tabs)) - 1
			} else {
				s.activeTab -= 1
			}
			return s, textinput.Blink
		}

	case tea.WindowSizeMsg:
		s.rawHeight = msg.Height
		s.rawWidth = msg.Width
		s.recalculateWindowMargins(msg.Width, msg.Height)

		recompileHelp(&s)
	}

	return s, s.tabs[s.activeTab].updateFunc(&s, msg)
}

// View displays the view function of the current tab.
func (s DataScope) View() string {
	if s.showTabs {
		return s.renderTabs(s.rawWidth) + "\n" + s.tabs[s.activeTab].viewFunc(&s)
	}
	return s.tabs[s.activeTab].viewFunc(&s)
}

// CobraNew creates a new bubble tea program, in alt buffer mode, running only the DataScope.
// For use from Cobra.Run() subroutines.
// Start the returned program via .Run().
func CobraNew(data []string, search *grav.Search, table bool, opts ...DataScopeOption,
) (p *tea.Program, err error) {
	ds, _, err := NewDataScope(data, false, search, table, opts...)
	if err != nil {
		return nil, err
	}
	return tea.NewProgram(ds, tea.WithAltScreen()), nil
}

// applies text wrapping to the given content. This is mandatory prior to SetContent, lest the text
// be clipped. It is a *possible* bug of the viewport bubble.
//
// (see:
// https://github.com/charmbracelet/bubbles/issues/479
// https://github.com/charmbracelet/bubbles/issues/56
// )
func wrap(width int, s string) string {
	return lipgloss.NewStyle().Width(width).Render(s)
}

// Updates the dimensions of datascope and recalculate usable spaces, considering margins.
// (ex: the tabs and a footer (if associated to the results/table tab)).
// Should be called after any changes to raw height or raw width.
func (s *DataScope) recalculateWindowMargins(rawWidth, rawHeight int) {
	// save the heights
	s.rawWidth, s.rawHeight = rawWidth, rawHeight

	var clippedHeight = rawHeight
	if s.showTabs {
		clippedHeight -= lipgloss.Height(s.renderTabs(s.rawWidth))
	}
	// inform the appropriate tab of the size change
	if s.tableMode {
		s.table.recalculateSize(rawWidth, clippedHeight)
	} else {
		s.results.recalculateSize(rawWidth, clippedHeight)
	}
}

// Returns the width of the terminal available for tabs to use, minus any margins reserved by the
// parent.
func (s *DataScope) usableWidth() int {
	return s.rawWidth
}

// Returns the height of the terminal available for tabs to use, minus any margins reserved by the
// parent (ex: the tab display)
func (s *DataScope) usableHeight() int {
	if s.showTabs {
		return s.rawHeight - lipgloss.Height(s.renderTabs(s.rawWidth))
	}
	return s.rawHeight
}
