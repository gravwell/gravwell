/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package attach

/* This file implements the "selecting view" of interactive attach.
Users are shown a list of attach-able searches and their statuses, can inspect each (a "details" view), and attach to one.
When a user attaches to a query, selecting view waits on it, only returning control to actor.go once the search is done.

To keep the list of attach-able searches up to date, a maintainer goroutine queries the backend periodically.
That bubbletea passes models around by value, rather than by reference, adds a significant complication that has shaped the design of the maintainer.

The original implementation has a goroutine running for each attachable search, pinging them independently.
This made adding and removing from the list, while maintaining indices, difficult and fell into the trap of the second implementation...

The second implementation had a single maintainer (like we do now) make direct adjustments to the list.
Due to bubbletea passing around models by value (but using a slice (a reference type (and all the baggage that brings)) under the hood),
interacting with the list directly from a different goroutine causes unintended behaviors (such as elements getting nil'd, but not actually removed from the list).
We could interact with the underlying array and change its elements,
but what subsection (/slice) of the array the current list instance pointed to was difficult to corral.

The current and final implementation is dead simple, but makes the assumption that ListSearchStatuses will always return stable-sorted results.

This implementation works ONLY because exactly one goroutine (the maintainer) makes structural changes to the list; the user goroutine only traverses it.
*/

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/busywait"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/listsupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
)

const (
	maintainerSleepDuration = time.Second * 2
)

type selectingView struct {
	errString string // an error to be displayed to users at the bottom of the screen. Wiped on KeyMsg.

	list list.Model // interact-able list displaying attach-able queries

	allDone      chan bool        // closed when selecting view is being destroyed
	updatedItems chan []list.Item // the new state of the items slice to replace the underlying array in the list

	searchErr chan error // closed when done
	spnr      spinner.Model
	search    *grav.Search // the current search we are waiting on (or nil)
}

// (Re-)initializes the view, clobbering existing data.
// Should be called whenever this view is entered (such as on attach startup).
func (sv *selectingView) init() (cmd tea.Cmd, noAttachables bool, err error) {
	// initialize variables
	sv.allDone = make(chan bool)
	sv.updatedItems = make(chan []list.Item)

	// build the list
	ss, err := connection.Client.ListSearchStatuses()
	if err != nil {
		clilog.Writer.Warnf("failed to get search status: %v", err)
		return nil, false, err
	} else if len(ss) == 0 {
		return nil, true, nil
	}

	if sv.list, err = spawnListAndMaintainer(ss, sv.allDone, sv.updatedItems); err != nil {
		return nil, false, err
	}

	//sv.list.Styles.HelpStyle = sv.list.Styles.HelpStyle.Width(sv.width)

	return uniques.FetchWindowSize, false, nil
}

// Destroys the state of the selecting view, killing any and all updater goroutines.
// You must call init() again or forge a new selectingView{}.
//
// ! Does not close the search!
func (sv *selectingView) destroy() {
	// close the done channel to alert goros to die
	if sv.allDone != nil {
		close(sv.allDone)
		sv.allDone = nil
	}
}

// Handles inputs for navigating the menu,
// transitioning to `details` mode or `attach` mode as required.
// Returns a search only once a search has been selected, attached to, and has results ready.
// If an error is returned, it is unrecoverable and control should be given back to Mother.
func (sv *selectingView) update(msg tea.Msg) (cmd tea.Cmd, finishedSearch *grav.Search, fatalErr error) {
	// when a resize arrives, update all styles and save of the new value
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		availHeight := msg.Height - (1 + int(heightMargin)) // carve out a line for the bottom text and include any desired margin
		// update heights
		sv.list.SetHeight(availHeight)
		sty.listAlign = sty.listAlign.MaxHeight(availHeight)
		sty.detailAlign = sty.detailAlign.MaxHeight(availHeight)

		// update widths
		halfWidth := (msg.Width / 2) - int(halfMargin)*2
		// fit the query and detail borders to the new half-width
		sty.qryWrap = sty.qryWrap.Width(halfWidth)
		sty.detailWrap = sty.detailWrap.Width(halfWidth)

		// set the list width
		sv.list.SetWidth(halfWidth)
		sv.list.Styles.HelpStyle = sv.list.Styles.HelpStyle.Width(halfWidth)

		// update the right side panes' styles
		//detailAlignSty = detailAlignSty.Width(dWidth)
	}

	// are we waiting on a search
	if sv.search != nil {
		// test if the search is done
		select {
		case err := <-sv.searchErr:
			return nil, sv.search, err
		default:
			sv.spnr, cmd = sv.spnr.Update(msg)
			return cmd, nil, nil
		}
	}

	// handle interacting with the list
	if msg, ok := msg.(tea.KeyMsg); ok {
		// clear any existing error
		sv.errString = ""

		switch msg.Type {
		case tea.KeySpace, tea.KeyEnter: // attach to the current item
			if err := sv.attachToQuery(); err != nil {
				return nil, nil, err
			}
			return sv.spnr.Tick, nil, nil
		}

	}
	// pass all other messages into the list
	sv.list, cmd = sv.list.Update(msg)
	// check for structural updates
	select {
	case itms := <-sv.updatedItems:
		clilog.Writer.Debugf("list updates arrived.")
		setItemsCmd := sv.list.SetItems(itms)
		if setItemsCmd != nil {
			cmd = tea.Batch(cmd, setItemsCmd)
		}
	default:
	}
	return cmd, nil, nil
}

func (sv *selectingView) view() string {

	// fetch the currently selected item
	var details string
	a, ok := sv.list.SelectedItem().(attachable)
	if !ok {
		clilog.Writer.Errorf("failed to cast selected item to attachable. Raw: %v", sv.list.SelectedItem())
		sv.errString = uniques.ErrGeneric.Error()
	} else {
		details = composeDetails(a)
	}

	var errSpnrHelp string // displays either the busywait spinner, an error, or help text on how to select
	if sv.search != nil {
		errSpnrHelp = sv.spnr.View()
	} else if sv.errString != "" {
		errSpnrHelp = sv.errString
	} else {
		errSpnrHelp = "Press space or enter to attach"
	}

	return lipgloss.JoinVertical(lipgloss.Center,
		lipgloss.JoinHorizontal(lipgloss.Center, // compose the left and right panes
			sty.listAlign.Render(sv.list.View()),
			sty.detailAlign.Render(details)),
		sty.bottomText.Render(errSpnrHelp))
}

//#region item

var _ listsupport.Item = attachable{}

// An attachable is just a wrapper around the SearchCtrlStatus type to allow us to fit it to the Item interface.
type attachable struct {
	types.SearchCtrlStatus
}

// One-line display of the given item
func (i attachable) Title() string {
	var status = string(i.State.Status)
	if status != "" {
		status = stylesheet.IndexStyle.Render("{" + string(i.State.Status) + "} ")
	}
	return fmt.Sprintf("%s%s", status, i.UserQuery)
}

func (i attachable) Description() string {
	return fmt.Sprintf("sID: %s | Global? %v", i.ID, i.Global)
}

// The string to substring against when a user filters the results.
func (i attachable) FilterValue() string {
	return i.UserQuery
}

//#endregion item

//#region helper subroutines

// helper subroutine for update. Called when a user chooses a query to attach to.
// Attempts to attach to the currently-highlighted item, then spins off a goroutine to wait on it waits on it.
//
// When this subroutine returns, the caller should entered a waiting state where it only handles and propagates sv.spnr.Tick()s until the searchErr channel receives a value or is closed.
func (sv *selectingView) attachToQuery() (fatalErr error) {
	itm, ok := sv.list.SelectedItem().(attachable)
	if !ok {
		clilog.Writer.Criticalf("failed to assert list item back to attachable. Raw item: %#v", sv.list.SelectedItem())
		return uniques.ErrGeneric
	}

	s, err := connection.Client.AttachSearch(itm.ID)
	if err != nil { // this error may be recoverable
		sv.errString = err.Error()
		return nil
	}
	sv.search = &s

	// close the updaters
	close(sv.allDone)
	sv.allDone = nil

	// prepare the error channel
	sv.searchErr = make(chan error)

	// spin off a goroutine to wait on the search
	go func() {
		clilog.Writer.Debugf("awaiting search %s", s.ID)
		err := connection.Client.WaitForSearch(s)
		clilog.Writer.Debugf("search %s is done (err: %v)", s.ID, err)
		if err != nil {
			sv.searchErr <- err
		}
		close(sv.searchErr)
	}()
	// start a spinner
	sv.spnr = busywait.NewSpinner()

	return nil
}

// composeDetails generates the right-hand side details pane for the given attachable (which should be the currently selected item).
// Stylizes each block (query and details) and pairs them, returning a single block of stylized text
func composeDetails(a attachable) string {
	// generate the query and warp it in a border
	wrappedQry := sty.qryWrap.Render(sty.qryBody.Render(a.UserQuery))

	// generate state text
	state := sty.state.Render(a.State.String())

	// generate the details body
	var detailSB strings.Builder
	detailSB.WriteString(fmt.Sprintf(
		sty.detailFieldText.Render("Range")+": %v --> %v\n\n"+
			sty.detailFieldText.Render("Started")+": %v\n"+
			sty.detailFieldText.Render("Clients")+": %d\n"+
			sty.detailFieldText.Render("Storage")+": %dB",
		a.StartRange.String(), a.EndRange.String(),
		a.LaunchInfo.Started,
		a.AttachedClients,
		a.StoredData))
	if a.NoHistory {
		detailSB.WriteString("\n" + stylesheet.Header2Style.Render("No History Mode"))
	}
	if a.Error != "" {
		detailSB.WriteString("\nError: " + stylesheet.ErrStyle.Render(a.Error))
	}

	// wrap detail view in a border
	wrappedDetails := sty.detailWrap.Render(sty.detailBody.Render(detailSB.String()))

	return lipgloss.JoinVertical(lipgloss.Center, wrappedQry, state, wrappedDetails)

}

// spawnListAndMaintainer generates and populates the list.Model of attachables and spins off a goroutine to maintain the list.
// The maintainer goroutine keeps the statuses of each attachable up to date  and checks for new attachables, appending them as they appear.
//
// Caller must supply (but not hold) the RWlock for interacting with the list as well as a channel that will be closed when the maintainer should shut down.
func spawnListAndMaintainer(ss []types.SearchCtrlStatus, done <-chan bool, updates chan<- []list.Item) (list.Model, error) {
	// wrap each item and create a list from the set of them
	itms := make([]list.Item, len(ss))
	for i, s := range ss {
		itms[i] = attachable{s}
	}
	l := listsupport.NewList(itms, 0, 0, "attach", "attach-ables")

	// NOTE(rlandau): we re-set items on creation as there appears to be a discrepancy between how list.New() and list.SetItems() update keybinds.
	// This can cause the UI to stutter when the first .SetItems occurs as .SetItems changes what keys are visible in the help section.
	// Instead, we just swallow that stutter immediately on creation so it isn't visible to the user.
	l.SetItems(itms)
	l.SetFilteringEnabled(false)

	// spin off the maintainer goro
	go func() {
		for {
			time.Sleep(maintainerSleepDuration)
			select {
			case <-done:
				clilog.Writer.Debugf("attach maintainer closing up shop")
				return
			default:
				// get the list of persistent searches
				ss, err := connection.Client.ListSearchStatuses()
				if err != nil {
					clilog.Writer.Warnf("attach maintainer failed to get search statuses: %v", err)
					continue
				}

				// coerce the statuses into attachables
				var attachables = make([]list.Item, len(ss))
				for i, status := range ss {
					attachables[i] = attachable{status}
				}

				updates <- attachables
			}
		}
	}()

	// return control to the caller
	return l, nil
}

//#endregion helper subroutines

//#region styles

// the margin between the list and composed details view; effectively doubled as it sets the center margin on both elements=
const (
	halfMargin   uint = 1
	heightMargin uint = 1
)

var sty struct {
	// Sets the style of the right-side pane query text, prior to wrapping it in a border
	qryBody lipgloss.Style
	// Sets the border around the qry body
	qryWrap lipgloss.Style
	// Colors and adds margins to the state displayed between the query and detail panes
	state lipgloss.Style
	// Colors the field titles in the details pane
	detailFieldText lipgloss.Style
	// Sets the style of the right-side pane details text, prior to wrapping it in a border
	detailBody lipgloss.Style
	// Sets the border around the detail body
	detailWrap lipgloss.Style

	// Used in conjunction with the other Aligns to compose the list element relative to other elements
	listAlign lipgloss.Style

	// Used in conjunction with the other Aligns to compose the completed details pane (qry+detail already bordered) relative to other elements
	detailAlign lipgloss.Style

	// Place and stylize the bottom text
	bottomText lipgloss.Style
} = struct {
	qryBody         lipgloss.Style
	qryWrap         lipgloss.Style
	state           lipgloss.Style
	detailFieldText lipgloss.Style
	detailBody      lipgloss.Style
	detailWrap      lipgloss.Style
	listAlign       lipgloss.Style
	detailAlign     lipgloss.Style
	bottomText      lipgloss.Style
}{
	qryBody:         lipgloss.NewStyle().Height(5).Margin(1),
	qryWrap:         stylesheet.Composable.Primary,
	state:           lipgloss.NewStyle().AlignHorizontal(lipgloss.Center).Margin(1, 0, 1).Foreground(stylesheet.AccentColor1),
	detailFieldText: stylesheet.Header1Style,
	detailBody:      lipgloss.NewStyle().Margin(1),
	detailWrap:      stylesheet.Composable.Secondary,
	listAlign:       lipgloss.NewStyle().AlignHorizontal(lipgloss.Left).MarginRight(int(halfMargin)),
	detailAlign:     lipgloss.NewStyle().MarginLeft(int(halfMargin)),
	bottomText:      lipgloss.NewStyle().AlignHorizontal(lipgloss.Center).Foreground(stylesheet.TertiaryColor).MaxHeight(1),
}

//#endregion styles
