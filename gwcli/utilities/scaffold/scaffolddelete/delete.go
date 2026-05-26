/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package scaffolddelete provides a template for building actions that delete/destroy data.

A delete action consumes a list of delete-able items, allowing the user to select one or more
interactively (via a multiselectlist) or by passing one or more IDs as bare arguments.

Delete actions have the --dryrun default flag.
*/
package scaffolddelete

import (
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/confirmation"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// DeleteFunc is the driver function for this action; it performs the (faux-, on dryrun) deletion once an item is picked.
type DeleteFunc[I scaffold.Id_t] func(dryrun bool, ID I) error

// FetchFunc is the precursor function; it fetches and formats the list of delete-able items.
type FetchFunc[I scaffold.Id_t] func() ([]multiselectlist.SelectableItem[I], error)

const (
	dryrunSuccessTextF = "DRYRUN: %v (ID %v) would have been deleted"
	deleteSuccessTextF = "%v (ID %v) deleted"
)

const heightBuffer = 4

// NewDeleteAction creates and returns a cobra.Command suitable for use as a delete action.
//
// Base flags:
//
//	--dryrun (SELECT, as a mock deletion)
//
// You must provide two functions to instantiate a generic delete:
//
// DeleteFunc is a function that performs the actual (mock) deletion.
// It is given the dryrun boolean (so it can select instead) and the IDs of the item to trash.
//
// FetchFunc is a function that fetches all delete-able records for the user to pick from.
// It is primarily used in interactive mode, as this is bypassed if a user states IDs as args.
func NewDeleteAction[I scaffold.Id_t](
	singular, plural string,
	del DeleteFunc[I],
	fch FetchFunc[I],
	opts Options) action.Pair {
	cmd := treeutils.GenerateAction(
		"delete",
		"delete one or more "+plural,
		"delete one or more "+plural+" by id or selection",
		[]string{},
		func(c *cobra.Command, s []string) error {
			// fetch values from flags
			IDs, dryrun, err := getFlags[I](c.Flags())
			if err != nil {
				return err
			}

			if len(IDs) == 0 {
				if noInteractive, err := c.Flags().GetBool(ft.NoInteractive.Name()); err != nil {
					return err
				} else if noInteractive {
					return errors.New(phrases.AtLeast1ArgRequired(plural))
				}
				// spin up mother
				return mother.Spawn(c.Root(), c, s)
			}

			// non-interactive: delete each given id
			var atLeastOneSuccess bool
			for _, ID := range IDs {
				if err := del(dryrun, ID); err != nil {
					fmt.Fprintf(c.ErrOrStderr(), "failed to delete %v (ID %v): %v", singular, ID, err)
					continue
				}
				atLeastOneSuccess = true
				if dryrun {
					fmt.Fprintf(c.OutOrStdout(), dryrunSuccessTextF+"\n", singular, ID)
				} else {
					fmt.Fprintf(c.OutOrStdout(), deleteSuccessTextF+"\n", singular, ID)
				}
			}
			if !atLeastOneSuccess {
				return errors.New("all operations failed")
			}
			return nil
		}, treeutils.GenerateActionOptions{Usage: ft.Optional("flags") + ""}) // TODO this should use the new ft.VariadicArgs
	fs := flags()
	cmd.Flags().AddFlagSet(&fs)
	opts.Apply(cmd)
	d := newDeleteModel(singular, plural, del, fch)
	return action.NewPair(cmd, d)
}

func flags() pflag.FlagSet {
	fs := pflag.FlagSet{}
	ft.Dryrun.Register(&fs)
	return fs
}

func getFlags[I scaffold.Id_t](fs *pflag.FlagSet) (ids []I, dryrun bool, _ error) {
	for _, s := range fs.Args() {
		id, err := scaffold.FromString[I](s)
		if err != nil {
			return nil, false, err
		}
		ids = append(ids, id)
	}
	if dr, err := fs.GetBool(ft.Dryrun.Name()); err != nil {
		return nil, false, err
	} else {
		dryrun = dr
	}

	return
}

//#region interactive mode (model) implementation

type mode uint

const (
	modeSelecting mode = iota
	modeConfirming
	modeDone
)

type deleteModel[I scaffold.Id_t] struct {
	itemSingular string // "macro", "kit", "query"
	itemPlural   string // "macros", "kits", "queries"
	mode         mode   // current mode
	dryrun       bool
	df           DeleteFunc[I] // function to delete an item
	ff           FetchFunc[I]  // function to get all delete-able items

	// selecting mode
	msl multiselectlist.Model[I]

	// confirming mode
	confirm confirmation.Model

	flagset pflag.FlagSet
}

func newDeleteModel[I scaffold.Id_t](singular, plural string, del DeleteFunc[I], fch FetchFunc[I]) *deleteModel[I] {
	d := &deleteModel[I]{
		itemSingular: singular,
		itemPlural:   plural,
		mode:         modeSelecting,
		df:           del,
		ff:           fch,
	}
	d.flagset = flags()
	return d
}

func (d *deleteModel[I]) Update(msg tea.Msg) tea.Cmd {
	if d.Done() {
		return nil
	}

	// always handle window size messages
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		wsm.Height = max(0, wsm.Height-heightBuffer)
		var cmds = make([]tea.Cmd, 2)
		d.msl, cmds[0] = d.msl.Update(wsm)
		d.confirm, cmds[1], _, _, _ = d.confirm.Update(wsm)
		return tea.Batch(cmds...)
	}

	var cmd tea.Cmd
	switch d.mode {
	case modeSelecting:
		d.msl, cmd = d.msl.Update(msg)
		if d.msl.Done() {
			d.msl.Undone() // in case we come back

			selected := d.msl.GetSelectedItems()
			if len(selected) < 1 {
				return d.msl.NewStatusMessage("you must select at least 1 " + d.itemSingular)
			}

			// If dryrun, skip confirmation and immediately report results
			if d.dryrun {
				d.mode = modeDone
				var cmds []tea.Cmd
				for _, itm := range selected {
					cmds = append(cmds, tea.Printf(dryrunSuccessTextF, d.itemSingular, itm.ID()))
				}
				return tea.Batch(cmds...)
			}

			// transition to confirmation
			d.mode = modeConfirming
			d.buildConfirmHeader()
		}
	case modeConfirming:
		var (
			done   bool
			submit bool
			choice uint
		)
		d.confirm, cmd, done, submit, choice = d.confirm.Update(msg)
		if !done {
			return cmd
		}
		if submit {
			// perform the actual deletions
			d.mode = modeDone
			selected := d.msl.GetSelectedItems()
			var resultCmds []tea.Cmd
			for _, itm := range selected {
				if err := d.df(false, itm.ID()); err != nil {
					resultCmds = append(resultCmds, tea.Printf("Failed to delete %v (ID %v): %v", d.itemSingular, itm.ID(), err))
				} else {
					resultCmds = append(resultCmds, tea.Printf(deleteSuccessTextF, d.itemSingular, itm.ID()))
				}
			}
			return tea.Batch(cmd, tea.Sequence(resultCmds...))
		}
		// choice 0 == return to list
		_ = choice
		d.mode = modeSelecting
	}

	return cmd
}

// buildConfirmHeader populates the confirmation bubble's header with a summary of pending deletions.
func (d *deleteModel[I]) buildConfirmHeader() {
	selected := d.msl.GetSelectedItems()
	var lines []string
	lines = append(lines, fmt.Sprintf("Deleting %d %v:", len(selected), d.itemPlural))
	for _, itm := range selected {
		lines = append(lines, fmt.Sprintf("  • %v", itm.Title()))
	}
	d.confirm.HeaderLines = lines
}

func (d *deleteModel[I]) View() string {
	switch d.mode {
	case modeDone:
		return "Deletion complete."
	case modeSelecting:
		return d.msl.View()
	case modeConfirming:
		return d.confirm.View()
	default:
		clilog.Writer.Warnf("Unknown mode %v", d.mode)
		return "An error has occurred. Exiting..."
	}
}

func (d *deleteModel[I]) Done() bool {
	return d.mode == modeDone
}

func (d *deleteModel[I]) Reset() error {
	d.mode = modeSelecting
	d.flagset = flags()
	d.msl = multiselectlist.Model[I]{}
	d.confirm = confirmation.Model{}
	return nil
}

func (d *deleteModel[I]) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (
	invalid string, onStart tea.Cmd, err error) {
	// fetch deleteable items
	itms, err := d.ff()
	if err != nil {
		return "", nil, err
	}

	// if there are no items to delete, die
	if len(itms) < 1 {
		d.mode = modeDone
		return "", tea.Printf("You have no %v that can be deleted", d.itemPlural), nil
	}

	adjustedHeight := max(0, height-heightBuffer)
	d.msl = multiselectlist.New(itms, width, adjustedHeight, multiselectlist.Options{})
	d.msl.SetShowStatusBar(true)
	d.msl.StatusMessageLifetime = stylesheet.StatusMessageLifetime
	d.msl.Title = "Delete " + d.itemPlural

	// initialize confirmation with a single choice: "item selection"
	d.confirm.Init([]string{"item selection"}, uint(width), uint(height))

	// parse flags
	if err := d.flagset.Parse(tokens); err != nil {
		return err.Error(), nil, nil
	}
	ids, dryrun, err := getFlags[I](&d.flagset)
	if err != nil {
		return "", nil, err
	}
	d.dryrun = dryrun

	if len(ids) > 0 {
		// Pre-select items by flag and skip directly to result
		d.mode = modeDone
		var resultCmds []tea.Cmd
		for _, id := range ids {
			if err := d.df(dryrun, id); err != nil {
				// check for sentinel errors
				if cerr, ok := err.(*client.ClientError); ok && cerr.StatusCode == 404 {
					resultCmds = append(resultCmds, tea.Printf("Did not find a valid %v with ID %v", d.itemSingular, id))
				} else {
					return "", nil, err
				}
			} else if dryrun {
				resultCmds = append(resultCmds, tea.Printf(dryrunSuccessTextF, d.itemSingular, id))
			} else {
				resultCmds = append(resultCmds, tea.Printf(deleteSuccessTextF, d.itemSingular, id))
			}
		}
		if len(resultCmds) == 0 {
			return "", nil, nil
		}
		return "", tea.Sequence(resultCmds...), nil
	}

	return "", nil, nil
}
