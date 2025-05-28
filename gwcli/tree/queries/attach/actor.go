package attach

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/tree/query/datascope"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/querysupport"
	"github.com/spf13/pflag"
)

/*
This file contains the action.Model implementation of the attach action, providing interactive usage.

Attach is some hideous amalgamation of the query actor and edit scaffolding actor.

Fits the tea.Model interface.
*/

const GenericErrorText string = "an error occurred"

//#region modes

// modes attach model can be in
type mode int8

const (
	inactive   mode = iota // prepared, but not utilized
	quitting               // leaving prompt
	selecting              // selecting persistent query to interact with; only returns when that search returns results
	displaying             // datascope is displaying results
)

//#endregion modes

type attach struct {
	mode mode // interactive attach state machine

	flagset pflag.FlagSet           // the set of flags; parsed and transmogrified when SetArgs is entered
	flags   querysupport.QueryFlags // the transmogrified form of flagset; contains flags attach does not use and thus are always zero

	// mode-specific state structs
	sv *selectingView

	search *grav.Search

	ds tea.Model
}

// Attach is the struct that provide bolt-on actions via the action map.
// It is technically a singleton, as the same struct is wiped and reused each invocation.
var Attach action.Model = Initial()

func Initial() *attach {
	a := &attach{mode: inactive}

	return a
}

// Update passes control down to selecting view or datascope, depending on the current mode.
// Handles transitioning from selecting -> displaying.
func (a *attach) Update(msg tea.Msg) tea.Cmd {
	switch a.mode {
	case quitting:
		return nil
	case inactive: // should not be possible, but if we are, bootstrap ourselves into selecting mode
	// TODO
	case displaying: // pass control to datascope
		if a.ds == nil {
			clilog.Writer.Errorf("attach cannot be in display mode without a valid datascope")
			a.mode = quitting
			return tea.Println(GenericErrorText)
		}
		var cmd tea.Cmd
		a.ds, cmd = a.ds.Update(msg)
		return cmd
	case selecting: // pass control to selecting view
		cmd, search, err := a.sv.update(msg)
		if err != nil {
			a.mode = quitting
			return tea.Println(err)
		} else if search != nil { // prepare datascope and hand off control
			a.mode = displaying
			a.search = search
			a.sv.destroy()

			results, tbl, err := querysupport.GetResultsForDataScope(search)
			if err != nil {
				a.mode = quitting
				return tea.Println(err)
			}
			var dsCmd tea.Cmd
			a.ds, dsCmd, err = datascope.NewDataScope(results, true, search, tbl)
			if err != nil {
				a.mode = quitting
				return tea.Println(err)
			}
			cmd = tea.Sequence(cmd, dsCmd)
			a.mode = displaying
		}
		return cmd
	}

	return nil
}

func (a *attach) View() string {
	switch a.mode {
	case quitting:
		return ""
	case displaying:
		return a.ds.View()
	case selecting:
		return a.sv.view()
	}

	return ""
}

func (a *attach) Done() bool {
	return a.mode == quitting
}

// Resetting attach returns it to the inactive state and clears temporary (pre-run) variables.
func (a *attach) Reset() error {
	a.mode = inactive
	a.ds = nil
	a.sv.destroy()
	a.sv = nil
	a.flagset = initialLocalFlagSet()
	if a.search != nil {
		a.search.Close()
	}

	return nil
}

// SetArgs allows interactive mode usage to fetch the pre-existing search by its id.
func (a *attach) SetArgs(_ *pflag.FlagSet, tokens []string) (invalid string, _ tea.Cmd, err error) {
	// parse the tokens against the local flagset
	if err := a.flagset.Parse(tokens); err != nil {
		return err.Error(), nil, nil
	}

	a.flags = querysupport.TransmogrifyFlags(&a.flagset)

	// if we are able to find a valid search, go directly to display mode or download the results and exit
	var sid string
	argCount := len(a.flagset.Args())
	if argCount == 1 {
		sid = strings.TrimSpace(a.flagset.Arg(0))
		s, err := connection.Client.AttachSearch(sid)
		if err != nil {
			if errors.Is(err, grav.ErrNotFound) {
				return querysupport.ErrUnknownSID(sid).Error(), nil, nil
			} else {
				return "", nil, err
			}
		}

		results, tblMode, err := querysupport.GetResultsForDataScope(&s)
		if err != nil {
			return "", nil, err
		}

		// jump directly into displaying
		var cmd tea.Cmd
		a.ds, cmd, err = datascope.NewDataScope(results, true, &s, tblMode,
			datascope.WithAutoDownload(a.flags.OutPath, a.flags.Append, a.flags.JSON, a.flags.CSV))
		if err != nil {
			clilog.Writer.Errorf("failed to create DataScope: %v", err)
			a.mode = quitting
			return "", nil, err
		}
		a.mode = displaying
		return "", cmd, nil
	} else if argCount > 1 {
		return errWrongArgCount(false), nil, nil
	}

	// build the mode structs
	a.sv = &selectingView{}

	// if a sid was not given, prepare a list of queries for the user to select from
	a.mode = selecting

	cmd, err := a.sv.init()
	if err != nil { // check that we actually have data to manipulate
		a.mode = quitting
		return "", tea.Println(err), nil
	}

	return "", cmd, nil
}
