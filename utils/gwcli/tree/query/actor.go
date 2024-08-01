/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package query

/**
 * This file contains the action.Model implemenation of the query action and coordinates the
 * interoperation of the composed editor and modifier views.
 */

import (
	"errors"
	"fmt"
	"gwcli/action"
	"gwcli/busywait"
	"gwcli/clilog"
	"gwcli/connection"
	"gwcli/stylesheet"
	"gwcli/tree/query/datascope"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	grav "github.com/gravwell/gravwell/v3/client"
	"github.com/spf13/pflag"
)

//#region modes

// modes query model can be in
type mode int8

const (
	inactive   mode = iota // prepared, but not utilized
	prompting              // accepting user input
	quitting               // leaving prompt
	waiting                // search submitted; waiting for results
	displaying             // datascope is displaying results
)

//#endregion modes

// interactive model definition
type query struct {
	mode mode

	// total screen sizes for composing subviews
	width  uint
	height uint

	editor editorView

	modifiers modifView

	flagModifiers struct { // flag options that only affect datascope
		json     bool
		csv      bool
		outfn    string
		append   bool
		schedule schedule
	}

	focusedEditor bool

	curSearch   *grav.Search // nil or ongoing/recently-completed search
	searchDone  atomic.Bool  // waiting thread has returned
	searchError chan error   // result to be fetched after SearchDone

	spnr  spinner.Model // wait spinner
	scope tea.Model     // interactively display data

	help help.Model

	keys []key.Binding // global keys, always active no matter the focused view

}

var Query action.Model = Initial()

func Initial() *query {
	q := &query{
		mode:        inactive,
		searchError: make(chan error),
		curSearch:   nil,
		spnr:        busywait.NewSpinner(),
	}

	// configure max dimensions
	q.width = 80
	q.height = 6

	q.editor = initialEdiorView(q.height, stylesheet.TIWidth)
	q.modifiers = initialModifView(q.height, q.width-stylesheet.TIWidth)

	q.focusedEditor = true

	q.keys = []key.Binding{
		key.NewBinding(key.WithKeys("tab"), // 0: cycle
			key.WithHelp("tab", "cycle view")),
		key.NewBinding(key.WithKeys("esc"), // [handled by mother]
			key.WithHelp("esc", "return to navigation")),
	}

	// set up help
	q.help = help.New()
	q.help.Width = int(q.width)

	BurnFirstView(q.editor.ta)

	return q
}

func (q *query) Update(msg tea.Msg) tea.Cmd {
	switch q.mode {
	case quitting:
		return textarea.Blink
	case displaying:
		if q.scope == nil {
			clilog.Writer.Errorf("query cannot be in display mode without a valid datascope")
			q.mode = quitting
		}
		// once we enter display mode, we do not leave until Mother kills us
		var cmd tea.Cmd
		q.scope, cmd = q.scope.Update(msg)
		return cmd
	case inactive: // if inactive, bootstrap
		q.mode = prompting
		q.editor.ta.Focus()
		q.focusedEditor = true
		return textarea.Blink
	case waiting: // display spinner and wait
		if q.searchDone.Load() { // search is done
			if err := <-q.searchError; err != nil { // failure, return to text input
				q.editor.err = err.Error()
				q.mode = prompting
				var cmd tea.Cmd
				q.editor.ta, cmd = q.editor.ta.Update(msg)
				return cmd
			}

			results, tableMode, err := fetchResults(q.curSearch)
			if err != nil {
				q.editor.err = err.Error()
				q.mode = prompting
				var cmd tea.Cmd
				q.editor.ta, cmd = q.editor.ta.Update(msg)
				return cmd
			} else if len(results) == 0 {
				q.mode = quitting
				return tea.Println(NoResultsText)
			}

			var cmd tea.Cmd
			// JSON,CSV,outfn,append are user-editable in the DataScope; these just set initial
			// values
			q.scope, cmd, err = datascope.NewDataScope(results, true, q.curSearch, tableMode,
				datascope.WithAutoDownload(
					q.flagModifiers.outfn,
					q.flagModifiers.append,
					q.flagModifiers.json,
					q.flagModifiers.csv),
				datascope.WithSchedule(
					q.flagModifiers.schedule.cronfreq,
					q.flagModifiers.schedule.name,
					q.flagModifiers.schedule.desc))
			if err != nil {
				clilog.Writer.Errorf("failed to create DataScope: %v", err)
				q.mode = quitting
				return tea.Println(err.Error())
			}

			q.mode = displaying
			return cmd
		}
		// still waiting
		var cmd tea.Cmd
		q.spnr, cmd = q.spnr.Update(msg)
		return cmd
	}

	// default, prompting mode

	keyMsg, isKeyMsg := msg.(tea.KeyMsg)

	// handle global keys
	if isKeyMsg {
		switch {
		case key.Matches(keyMsg, q.keys[0]):
			q.switchFocus()
		}
	}

	// pass message to the active view
	var cmds []tea.Cmd
	if q.focusedEditor { // editor view active
		c, submit := q.editor.update(msg)
		if submit {
			return q.submitQuery()
		}
		cmds = []tea.Cmd{c}
	} else { // modifiers view active
		cmds = q.modifiers.update(msg)
	}

	return tea.Batch(cmds...)
}

func (q *query) View() string {
	if q.mode == displaying {
		return q.scope.View()
	}

	var blankOrSpnr string
	if q.mode == waiting { // if waiting, show a spinner instead of help
		blankOrSpnr = q.spnr.View()
	} else {
		blankOrSpnr = "\n"
	}

	var (
		viewKeys     []key.Binding
		editorView   string
		modifierView string
	)
	if q.focusedEditor {
		viewKeys = q.editor.keys
		editorView = stylesheet.Composable.Focused.Render(q.editor.view())
		modifierView = stylesheet.Composable.Unfocused.Render(q.modifiers.view())
	} else {
		viewKeys = q.modifiers.keys
		editorView = stylesheet.Composable.Unfocused.Render(q.editor.view())
		modifierView = stylesheet.Composable.Focused.Render(q.modifiers.view())
	}
	h := q.help.ShortHelpView(append(q.keys, viewKeys...))

	return fmt.Sprintf("%s\n%s\n%s",
		lipgloss.JoinHorizontal(lipgloss.Top, editorView, modifierView),
		h,
		blankOrSpnr)
}

func (q *query) Done() bool {
	return q.mode == quitting
}

func (q *query) Reset() error {
	// ! all inputs are blurred until user re-enters query later

	q.mode = inactive

	// reset editor view
	q.editor.ta.Reset()
	q.editor.err = ""
	q.editor.ta.Blur()
	// reset modifier view
	q.modifiers.reset()

	// clear query fields
	q.curSearch = nil
	q.searchDone.Store(false)
	q.scope = nil

	localFS = initialLocalFlagSet()

	return nil
}

// Consume flags and associated them to the local flagset
func (q *query) SetArgs(_ *pflag.FlagSet, tokens []string) (string, tea.Cmd, error) {
	// parse the tokens agains the local flagset
	if err := localFS.Parse(tokens); err != nil {
		return err.Error(), nil, nil
	}

	flags, err := transmogrifyFlags(&localFS)
	if err != nil {
		return "", nil, err
	}

	// check for script mode (invalid, as Mother is already running)
	if flags.script {
		return "", nil, errors.New("cannot invoke script mode while in interactive mode")
	}

	// set fields by flags
	q.modifiers.durationTI.SetValue(flags.duration.String())
	q.flagModifiers.json = flags.json
	q.flagModifiers.csv = flags.csv
	q.flagModifiers.outfn = flags.outfn
	q.flagModifiers.append = flags.append
	q.flagModifiers.schedule = flags.schedule

	// TODO pull qry from referenceID, if given

	if qry := strings.TrimSpace(strings.Join(localFS.Args(), " ")); qry != "" {
		q.editor.ta.SetValue(qry)
		// if we are given a query, submitQuery will place us directly into waiting mode
		return "", q.submitQuery(), nil
	}

	return "", nil, nil
}

//#region helper subroutines

// Gathers information across both views and initiates the search, placing the model into a waiting
// state. A seperate goroutine, initialized here, waits on the search, allowing this thread to
// display a spinner.
// Corrollary to `outputSearchResults` (connected via `case waiting` in Update()).
func (q *query) submitQuery() tea.Cmd {
	qry := q.editor.ta.Value() // clarity

	clilog.Writer.Infof("Submitting query '%v'...", qry)

	// fetch modifiers from alternative view
	var (
		duration time.Duration
		err      error
	)
	if d := strings.TrimSpace(q.modifiers.durationTI.Value()); d != "" {
		duration, err = time.ParseDuration(q.modifiers.durationTI.Value())
		if err != nil {
			q.editor.err = err.Error()
			return nil
		}
	} else {
		duration = defaultDuration
	}

	s, err := connection.StartQuery(qry, -duration)
	if err != nil {
		q.editor.err = err.Error()
		return nil
	}

	// spin up a goroutine to wait on the search while we show a spinner
	go func() {
		err := connection.Client.WaitForSearch(s)
		// notify we are done and buffer the error for retrieval
		q.searchDone.Store(true)
		q.searchError <- err
	}()

	q.curSearch = &s
	q.mode = waiting
	return q.spnr.Tick // start the wait spinner
}

// swaps between focusing the editor and focusing the modifiers
func (q *query) switchFocus() {
	q.focusedEditor = !q.focusedEditor
	if q.focusedEditor { // disable viewB interactions
		q.modifiers.durationTI.Blur()
		q.editor.ta.Focus()
	} else { // disable query editor interaction
		q.editor.ta.Blur()
		q.modifiers.durationTI.Focus()
	}
}

//#endregion helper subroutines

func BurnFirstView(ta textarea.Model) {

	/**
	 * Omitting this superfluous view outputs rgb control characters to the *first* instance of the
	 * query editor.
	 */
	_ = ta.View()

	/**
	 * A deeper dive:
	 * Formerly, Actions, particularly actions with TextArea/TextInputs hung the first time one was
	 * invoked each time the program launched. They eventually redrew, fixing the issue, but it
	 * could take quite a while.
	 * What was weird was that it was *not* that each one hung, but that the first hung and then all
	 * actions thereafter were fine. In other words, it was either related to a costly
	 * initialization in TA/TIs or not properly triggering redraws (by not sending tea.Cmds were we
	 * should be).
	 * The errant view call above was wrapped in a goroutine
	 * (`go func() { q.editor.ta.View() }()`)
	 * and it paid the startup cost in a way invisible to the user so the UX was seamless.
	 * Some optimizations and reworks later, and I figued out that the hang/redraw issue was likely
	 * due to missing tea.Cmds (the latter of the possibilities above).
	 *
	 * However, I also discovered that the go .view instruction was causing garbage (rgb control
	 * characters) to be output to the terminal if Mother was not invoked to catch the characters.
	 * This caused *some* non-interactive commands to output garbage to the users terminal or, worst
	 * case, break older shells (such as `sh`).
	 *
	 * The RGB control characters issue still persists and eliminating the above call causes garbage
	 * to appear in the first, interactive call to query.
	 * I have looked into the issue and it seems to stem from termenv.
	 * These characters are requested by termenv on startup to determine the capabilities of the
	 * terminal, but can be output to the terminal if term latency is too high.
	 * Supposedly this issue was fixed in termenv in [2021](https://github.com/muesli/termenv/pull/27).
	 * This means one of two things: the issue is not as resovled as it seems or, more likely, we or
	 * lipgloss are doing something ill-advised that causes these characters to not be collected by
	 * termenv properly.
	 *
	 * I would love to know what the issue is and hope to dedicate time to delving into termenv and
	 * lipgloss to investigate, but termenv is a doozy and my time is better spent elsewhere, as
	 * this band-aid is doing its job for minimal technical debt.
	 */

}
