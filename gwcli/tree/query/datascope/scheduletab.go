/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datascope

/**
 * The schedule tab is where a user can schedule the query that proc'd this DS session.
 * Really just some TIs with mild input validation.
 */

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/colorizer"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type scheduleCursor = uint // current active item

const (
	schlowBound scheduleCursor = iota
	schcronfreq
	schname
	schdesc
	schhighBound
)

type scheduleTab struct {
	selected         scheduleCursor
	resultString     string // result of the previous scheduling
	inputErrorString string // issues with current user input

	cronfreqTI textinput.Model
	nameTI     textinput.Model
	descTI     textinput.Model
}

// Initializes and returns a ScheduleTab struct suitable for representing the schedule options.
func initScheduleTab(cronfreq, name, desc string) scheduleTab {
	sch := scheduleTab{
		cronfreqTI: stylesheet.NewTI("", false),
		nameTI:     stylesheet.NewTI(name, false),
		descTI:     stylesheet.NewTI(desc, false),
	}

	// set TI-specific options
	sch.cronfreqTI.Placeholder = "* * * * *"
	sch.cronfreqTI.Validate = uniques.CronRuneValidator

	// focus frequency by default
	sch.cronfreqTI.SetValue(cronfreq)
	sch.cronfreqTI.Focus()
	sch.selected = schcronfreq

	return sch
}

// Update handles moving the cursor and passing messages to all 3 TIs.
func updateSchedule(s *DataScope, msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyMsg); ok {
		s.schedule.inputErrorString = ""
		switch msg.Type {
		case tea.KeyUp:
			s.schedule.selected -= 1
			if s.schedule.selected <= schlowBound {
				s.schedule.selected = schhighBound - 1
			}
			s.schedule.focusSelected()
			return textinput.Blink
		case tea.KeyDown:
			s.schedule.selected += 1
			if s.schedule.selected >= schhighBound {
				s.schedule.selected = schlowBound + 1
			}
			s.schedule.focusSelected()
			return textinput.Blink
		case tea.KeyEnter:
			if msg.Alt { // only accept alt+enter
				s.sch()
				return nil
			}
		}
	}

	// pass onto the TIs
	var cmds = make([]tea.Cmd, 3)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		s.schedule.cronfreqTI, cmds[0] = s.schedule.cronfreqTI.Update(msg)
	}()
	go func() {
		defer wg.Done()
		s.schedule.nameTI, cmds[1] = s.schedule.nameTI.Update(msg)
	}()
	go func() {
		defer wg.Done()
		s.schedule.descTI, cmds[2] = s.schedule.descTI.Update(msg)
	}()

	wg.Wait()

	return tea.Batch(cmds...)
}

func viewSchedule(s *DataScope) string {
	sel := s.schedule.selected // brevity

	var (
		titleSty       = stylesheet.Header1Style
		leftAlignerSty = lipgloss.NewStyle().
				Width(20).
				AlignHorizontal(lipgloss.Right).
				PaddingRight(1)
	)

	tabDesc := tabDescStyle(s.usableWidth()).Render("Schedule this search to be rerun at" +
		" consistent intervals." + "\nQuery: " + stylesheet.Header2Style.Render(s.search.UserQuery))

	// build the field names column
	fields := lipgloss.JoinVertical(lipgloss.Right,
		leftAlignerSty.Render(fmt.Sprintf("%s%s",
			colorizer.Pip(sel, schcronfreq), titleSty.Render("Frequency:"))),
		leftAlignerSty.Render(fmt.Sprintf("%s%s",
			colorizer.Pip(sel, schname), titleSty.Render("Name:"))),
		leftAlignerSty.Render(fmt.Sprintf("%s%s",
			colorizer.Pip(sel, schdesc), titleSty.Render("Description:"))),
	)

	// build the TIs column
	TIs := lipgloss.JoinVertical(lipgloss.Left,
		s.schedule.cronfreqTI.View(),
		s.schedule.nameTI.View(),
		s.schedule.descTI.View(),
	)

	composed := lipgloss.JoinHorizontal(lipgloss.Center,
		fields,
		TIs)

	return lipgloss.Place(s.usableWidth(), s.usableHeight(),
		lipgloss.Center, verticalPlace,
		lipgloss.JoinVertical(lipgloss.Center,
			tabDesc,
			composed,
			"",
			colorizer.SubmitString("alt+enter", s.schedule.inputErrorString, s.schedule.resultString, s.usableWidth()),
		),
	)
}

// The actual scheduling driver that consumes the user inputs and attempts to schedule a new search.
// Sets resultString, inputErrorString and prints to clilog automatically.
func (s *DataScope) sch() {
	// gather and validate selections
	var (
		n   = strings.TrimSpace(s.schedule.nameTI.Value())
		d   = strings.TrimSpace(s.schedule.descTI.Value())
		cf  = strings.TrimSpace(s.schedule.cronfreqTI.Value())
		qry = s.search.UserQuery
	)
	// fetch the duration from the search struct
	start, end := s.search.SearchRange()

	id, invalid, err := connection.CreateScheduledSearch(n, d, cf, qry, end.Sub(start))
	if invalid != "" { // bad parameters
		s.schedule.inputErrorString = invalid
		clilog.Writer.Debug(s.schedule.inputErrorString)
	} else if err != nil {
		s.schedule.resultString = err.Error()
		clilog.Writer.Error(err.Error())
	} else {
		s.schedule.resultString = fmt.Sprintf("successfully scheduled query (ID: %v)", id)
		clilog.Writer.Info(s.schedule.resultString)
	}
}

// Focuses the TI corresponding to sch.selected and blurs all others.
func (sch *scheduleTab) focusSelected() {
	switch sch.selected {
	case schcronfreq:
		sch.cronfreqTI.Focus()
		sch.nameTI.Blur()
		sch.descTI.Blur()
	case schname:
		sch.cronfreqTI.Blur()
		sch.nameTI.Focus()
		sch.descTI.Blur()
	case schdesc:
		sch.cronfreqTI.Blur()
		sch.nameTI.Blur()
		sch.descTI.Focus()
	}
}
