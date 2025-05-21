package attach

import (
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

//#region modes

// modes attach model can be in
type mode int8

const (
	inactive   mode = iota // prepared, but not utilized
	quitting               // leaving prompt
	selecting              // selecting persistent query to interact with
	editing                // editing a selected query
	displaying             // datascope is displaying results
)

//#endregion modes

type attach struct {
	mode mode

	flagset pflag.FlagSet           // the set of flags; parsed and transmogrified when SetArgs is entered
	flags   querysupport.QueryFlags // the transmogrified form of flagset; contains flags attach does not use and thus are always zero

	search *grav.Search

	scope tea.Model // datascope for displaying data
}

// Attach is the struct that provide bolt-on actions via the action map.
// It is technically a singleton, as the same struct is wiped and reused each invocation.
var Attach action.Model = Initial()

func Initial() *attach {
	a := &attach{mode: inactive}

	return a
}

func (a *attach) Update(msg tea.Msg) tea.Cmd {
	// TODO
	switch a.mode {

	}

	return nil
}

func (a *attach) View() string {
	// TODO
	switch a.mode {

	}

	return ""
}

func (a *attach) Done() bool {
	return a.mode == quitting
}

func (a *attach) Reset() error {
	a.mode = inactive

	/*
		// reset editor view
		q.editor.ta.Reset()
		q.editor.err = ""
		q.editor.ta.Blur()
		// reset modifier view
		q.modifiers.reset()

		// clear query fields
		q.curSearch = nil
		q.searchDone.Store(false)
		// if there was an existing datascope, close its channel to signal the KeepAlive goro to die
		if q.scope != nil {
			if ds, ok := q.scope.(datascope.DataScope); ok {
				close(ds.Done)
			}
			q.scope = nil
		}
	*/
	a.flagset = initialLocalFlagSet()

	return nil
}

// SetArgs allows interactive mode usage to fetch the pre-existing search by its id.
func (a *attach) SetArgs(p *pflag.FlagSet, tokens []string) (invalid string, _ tea.Cmd, err error) {
	// parse the tokens against the local flagset
	if err := a.flagset.Parse(tokens); err != nil {
		return err.Error(), nil, nil
	}

	a.flags = querysupport.TransmogrifyFlags(&a.flagset)

	// if we are able to find a valid search, go directly to display mode or download the results and exit
	var sid string
	argCount := len(p.Args())
	if argCount == 1 {
		sid = strings.TrimSpace(p.Arg(0))
		s, err := connection.Client.AttachSearch(sid)
		if err != nil {
			// TODO if this is an unknown search error, return it as invalid
			return "", nil, err
		}
		a.search = &s

		results, tblMode, err := querysupport.FetchSearchResults(a.search)
		if err != nil {
			return "", nil, err
		}

		// TODO if we were given an output, spit the results into it and return to Mother

		// jump directly into displaying
		var cmd tea.Cmd
		a.scope, cmd, err = datascope.NewDataScope(results, true, a.search, tblMode,
			datascope.WithAutoDownload(a.flags.OutPath, a.flags.Append, a.flags.JSON, a.flags.CSV))
		if err != nil {
			clilog.Writer.Errorf("failed to create DataScope: %v", err)
			a.mode = quitting
			return "", nil, err
		}
		a.mode = displaying
		return "", cmd, nil
	} else if argCount > 1 {
		return errWrongInteractiveArgCount(), nil, nil
	}

	a.mode = selecting
	return "", nil, nil
}
