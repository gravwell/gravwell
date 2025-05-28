package attach

import (
	"errors"
	"fmt"
	"sync"

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
	listHeightMax = 40
)

// This file covers the `selecting` state,
// where a user sees an overview of all attachable searches
// and can select one to attach to.

type selectingView struct {
	errString string

	width, height int // tty dimensions, queried by init()

	list     list.Model // interact-able list display attach-able queries; created by transmuting the searches map
	selected string     // the sid of the item the user selected to attach to or view details for

	// current list of searches to select from
	searches []types.SearchCtrlStatus
	allDone  chan bool    // closed when selecting view is being destroyed
	listMu   sync.RWMutex // held whenever the list is touched

	searchErr chan error // closed when done
	spnr      spinner.Model
	search    *grav.Search // the current search we are waiting on (or nil)
}

// (Re-)initializes the view, clobbering existing data.
// Should be called whenever this view is entered (such as on attach startup).
func (sv *selectingView) init() (cmd tea.Cmd, err error) {
	// fetch attachables
	if err := sv.refreshSearches(); err != nil {
		return nil, err
	} else if len(sv.searches) <= 0 {
		return nil, errors.New("you have no attachable searches")
	}

	sv.allDone = make(chan bool)

	// acquire the list lock to prevent goros from starting too early
	sv.listMu.Lock()
	defer sv.listMu.Unlock()

	itms := make([]list.Item, len(sv.searches))
	for i, s := range sv.searches {
		a := attachable{s}
		itms[i] = a
		// spin off a goroutine to keep this item up to date
		go func(itmIdx int, a attachable, done <-chan bool) {
			// update until the search is done or errors
			for a.State.Status != types.SearchStatusCompleted && a.State.Status != types.SearchStatusError {
				select {
				case <-done: // check if we are done
					clilog.Writer.Debugf("updater %d closing up shop", itmIdx)
					return
				default:
					// get the status of the search
					newStatus, err := connection.Client.SearchStatus(a.ID)
					if err != nil {
						clilog.Writer.Errorf("updater %d failed to get the status of search %v: %v", i, a.ID, err)
						return
					}
					// if the status is different their our current status, replace it in the list
					if a.State.Status != newStatus.State.Status {
						clilog.Writer.Debugf("updater %d found new status: %v", i, newStatus.State.Status)
						sv.listMu.Lock()
						a = attachable{newStatus}
						sv.list.SetItem(i, a) // TODO do we need the returned command?
						sv.listMu.Unlock()
					}
				}
			}
			clilog.Writer.Debugf("updater %d retiring...", i)
		}(i, a, sv.allDone)
	}

	// build the list skeleton
	sv.list = listsupport.NewList(itms, 80, listHeightMax, "attach", "attach-ables")

	return uniques.FetchWindowSize, nil
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
	// handle resizes
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		sv.width = msg.Width
		sv.height = msg.Height

		sv.list.SetHeight(min(msg.Height-2, listHeightMax))
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
		case tea.KeyRight: // examine the current item
			// TODO enter details mode
		case tea.KeySpace, tea.KeyEnter: // attach to the current item
			if err := sv.attachToQuery(); err != nil {
				return nil, nil, err
			}
			return sv.spnr.Tick, nil, nil
		}
	}
	// pass all other messages into the list
	sv.list, cmd = sv.list.Update(msg)
	return cmd, nil, nil
}

func (sv *selectingView) view() string {
	var errSpnrHelp string // displays either the busywait spinner, an error, or help text on how to select
	if sv.search != nil {
		errSpnrHelp = sv.spnr.View()
	} else if sv.errString != "" {
		errSpnrHelp = sv.errString
	} else {
		errSpnrHelp = "Press space or enter to attach"
	}

	sv.listMu.RLock()
	defer sv.listMu.RUnlock()
	return sv.list.View() + "\n" +
		lipgloss.NewStyle().
			AlignHorizontal(lipgloss.Center).
			Width(sv.width).
			Foreground(stylesheet.TertiaryColor).
			Render(errSpnrHelp)
}

//#region item

var _ listsupport.Item = attachable{}

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

// helper subroutine for update. Called when // TODO
func (sv *selectingView) attachToQuery() (fatalErr error) {
	sv.listMu.RLock()
	itm, ok := sv.list.SelectedItem().(attachable)
	sv.listMu.RUnlock()
	if !ok {
		clilog.Writer.Criticalf("failed to assert list item back to attachable. Raw item: %#v", sv.list.SelectedItem())
		return errors.New(GenericErrorText)
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

// Fetches the list of available searches from the backend again,
// refreshing sv.searches.
func (sv *selectingView) refreshSearches() error {
	ss, err := connection.Client.ListSearchStatuses()
	if err != nil {
		return err
	}

	// update all searches
	sv.searches = ss // TODO do we have to cache searches?

	return nil

}

//#endregion helper subroutines
