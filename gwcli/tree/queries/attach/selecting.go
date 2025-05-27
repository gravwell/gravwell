package attach

import (
	"errors"

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

	searchErr chan error // closed when done
	spnr      spinner.Model
	search    *grav.Search // the current search we are waiting on (or nil)
}

// (Re-)initializes the view, clobbering existing data.
// Should be called whenever this view is entered (such as on attach startup).
func (sv *selectingView) init() (cmd tea.Cmd, count int) {
	// fetch attachables
	if err := sv.refreshSearches(); err != nil {
		sv.errString = err.Error()
		return nil, -1
	}

	itms := make([]list.Item, len(sv.searches))
	for i, s := range sv.searches {
		itms[i] = attachable{s.ID, s.UserQuery, s.State.String()}
	}

	// build the list skeleton
	sv.list = listsupport.NewList(itms, 80, listHeightMax, "attach", "attach-ables")

	return uniques.FetchWindowSize, len(sv.searches)
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
			return sv.spnr.Tick, nil, nil
		}
	}

	// handle interacting with the list
	if msg, ok := msg.(tea.KeyMsg); ok {
		// clear any existing errors
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
	var errSpnr string
	if sv.search != nil {
		errSpnr = sv.spnr.View()
	} else {
		errSpnr = sv.errString
	}

	return sv.list.View() + "\n" + errSpnr + "\n" +
		lipgloss.NewStyle().
			AlignHorizontal(lipgloss.Center).
			Width(sv.width).
			Foreground(stylesheet.TertiaryColor).
			Render("Press space or enter to attach")
}

// Fetches the list of available searches from the backend again,
// refreshing sv.searches.
func (sv *selectingView) refreshSearches() error {
	ss, err := connection.Client.ListSearchStatuses()
	if err != nil {
		return err
	}

	// update all searches
	sv.searches = ss

	return nil

}

//#region item

var _ listsupport.Item = attachable{}

type attachable struct {
	id    string
	query string
	state string
}

func (i attachable) Title() string {
	return i.id // TODO
}

func (i attachable) Description() string {
	return i.query // TODO
}

func (i attachable) FilterValue() string {
	return i.query
}

//#endregion item

//#region helper subroutines

// helper subroutine for update. Called when
func (sv *selectingView) attachToQuery() (fatalErr error) {
	itm, ok := sv.list.SelectedItem().(attachable)
	if !ok {
		clilog.Writer.Criticalf("failed to assert list item back to attachable. Raw item: %#v", sv.list.SelectedItem())
		return errors.New(GenericErrorText)
	}

	s, err := connection.Client.AttachSearch(itm.id)
	if err != nil { // this error may be recoverable
		sv.errString = err.Error()
		return nil
	}
	sv.search = &s
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

//#endregion helper subroutines
