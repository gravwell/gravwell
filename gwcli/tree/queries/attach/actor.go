package attach

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/spf13/pflag"
)

/**
This file contains the action.Model implementation of the attach action, providing interactive usage.
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
	mode   mode
	search client.Search

	initialFS pflag.FlagSet
}

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
	a.initialFS = initialLocalFlagSet()

	return nil
}

// SetArgs allows interactive mode usage to fetch the pre-existing search by its id.
func (a *attach) SetArgs(p *pflag.FlagSet, _ []string) (invalid string, _ tea.Cmd, err error) {
	// strip id out of bare args
	var sid string
	{
		argCount := len(p.Args())
		if argCount != 1 {
			return fmt.Sprintf("attach takes exactly one argument (found %d)", argCount), nil, nil
		}
		sid = p.Arg(0)
	}

	s, err := connection.Client.AttachSearch(sid)
	if err != nil {
		// TODO if this is an unknown search error, return it as invalid
		return "", nil, err
	}
	a.search = s

	// spin up the datascope

	return "", nil, nil
}
