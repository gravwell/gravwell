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

	"github.com/dustin/go-humanize/english"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/confirmation"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/state"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// DeleteFunc is the driver function for this action; it performs the (faux-, on dryrun) deletion once an item is picked.
type DeleteFunc[I scaffold.Id_t] func(dryrun bool, ID I, fs *pflag.FlagSet) error

// FetchFunc is the precursor function; it fetches and formats the list of delete-able items.
// It is called iff we enter interactive mode.
// If the action is non-interactive or direct-invoked and IDs are given, we skip directly to the DeleteFunc.
type FetchFunc[I scaffold.Id_t] func(param DataParameters) ([]multiselectlist.SelectableItem[I], error)

const (
	DryrunSuccessTextF = "DRYRUN: %v (ID: %v) would have been deleted"
	DeleteSuccessTextF = "%v (ID: %v) deleted"
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
func NewDeleteAction[I scaffold.Id_t](singular string, del DeleteFunc[I], fch FetchFunc[I], opts Options) action.Pair {
	plural := english.PluralWord(2, singular, "")
	var usage string
	if opts.AddtlFlags != nil {
		usage = ft.Optional("FLAGS")
	} else {
		usage = ft.Optional("--dryrun")
	}
	usage += " " + ft.VariadicArgs(singular, true)

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
				if state.Interactive() {
					return mother.Spawn(c.Root(), c, s)
				}
				return errors.New(phrases.AtLeast1ArgRequired(plural))
			}

			// non-interactive: delete each given id
			var atLeastOneSuccess bool
			results := attemptDeletions(singular, IDs, dryrun, del, c.Flags())
			for _, res := range results {
				if res.err != nil {
					fmt.Fprintln(c.ErrOrStderr(), res.err.Error())
				} else {
					fmt.Fprintln(c.OutOrStdout(), res.success)
					atLeastOneSuccess = true
				}
			}
			if !atLeastOneSuccess {
				return errors.New("all operations failed")
			}
			return nil
		}, treeutils.GenerateActionOptions{Usage: usage})
	fs := flags()
	cmd.Flags().AddFlagSet(&fs)
	if opts.QueryOptionsFlags != nil {
		opts.QueryOptionsFlags.Install(cmd.Flags())

	}
	opts.Apply(cmd)
	d := &deleteModel[I]{
		itemSingular: singular,
		itemPlural:   plural,
		mode:         modeSelecting,
		del:          del,
		fch:          fch,
		options:      opts,
	}
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

// attemptDeletion is the actual deletion actor, used to keep the behavior of each entry point uniform.
func attemptDeletions[I scaffold.Id_t](singular string, IDs []I, dryrun bool, del DeleteFunc[I], fs *pflag.FlagSet) (results []struct {
	success string
	err     error
}) {
	results = make([]struct {
		success string
		err     error
	}, len(IDs))
	for i, ID := range IDs {
		if err := del(dryrun, ID, fs); err != nil {
			if phrases.IsNotFoundErr(err) {
				results[i] = struct {
					success string
					err     error
				}{"", phrases.ErrUnknownIdentifier(ID, singular)}
			} else {
				results[i] = struct {
					success string
					err     error
				}{
					"", fmt.Errorf("failed to delete %v (ID %v): %v", singular, ID, err),
				}
			}
			continue
		}
		// success
		if dryrun {
			results[i] = struct {
				success string
				err     error
			}{
				fmt.Sprintf(DryrunSuccessTextF, singular, ID), nil,
			}
		} else {
			results[i] = struct {
				success string
				err     error
			}{
				fmt.Sprintf(DeleteSuccessTextF, singular, ID), nil,
			}
		}
	}
	return results
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
	del          DeleteFunc[I] // function to delete an item
	fch          FetchFunc[I]  // function to get all delete-able items
	options      Options

	// selecting mode
	msl multiselectlist.Model[I]

	// confirming mode
	confirm confirmation.Model

	flagset pflag.FlagSet
}

func (d *deleteModel[I]) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (
	invalid string, onStart tea.Cmd, err error) {
	// parse flags
	d.flagset = flags()
	if d.options.AddtlFlags != nil {
		d.flagset.AddFlagSet(d.options.AddtlFlags())
	}
	if d.options.QueryOptionsFlags != nil {
		d.options.QueryOptionsFlags.Install(&d.flagset)
	}
	if err := d.flagset.Parse(tokens); err != nil {
		return err.Error(), nil, nil
	}
	IDs, dryrun, err := getFlags[I](&d.flagset)
	if err != nil {
		return "", nil, err
	}
	d.dryrun = dryrun

	if len(IDs) > 0 {
		// Pre-select items by flag and skip directly to result
		d.mode = modeDone
		var atLeastOneSuccess bool
		results := attemptDeletions(d.itemSingular, IDs, d.dryrun, d.del, &d.flagset)
		cmds := make([]tea.Cmd, len(results))
		for i, res := range results {
			if res.err != nil {
				cmds[i] = tea.Println(res.err.Error())
			} else {
				cmds[i] = tea.Println(res.success)
				atLeastOneSuccess = true
			}
		}
		if !atLeastOneSuccess {
			cmds = append(cmds, tea.Println("all operations failed"))
		}
		return "", tea.Sequence(cmds...), nil
	}

	// fetch deleteable items
	params := DataParameters{}
	if d.options.QueryOptionsFlags != nil {
		params.QueryOpts = d.options.QueryOptionsFlags.QueryOptions(&d.flagset)
	}
	itms, err := d.fch(params)
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

	return "", nil, nil
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

			if len(d.msl.GetSelectedItems()) < 1 {
				return d.msl.NewStatusMessage("you must select at least 1 " + d.itemSingular)
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
			IDs := make([]I, len(selected))
			for i, sel := range selected {
				IDs[i] = sel.ID()
			}
			var atLeastOneSuccess bool
			results := attemptDeletions(d.itemSingular, IDs, d.dryrun, d.del, &d.flagset)
			cmds := make([]tea.Cmd, len(results))
			for i, res := range results {
				if res.err != nil {
					cmds[i] = tea.Println(res.err.Error())
				} else {
					cmds[i] = tea.Println(res.success)
					atLeastOneSuccess = true
				}
			}
			if !atLeastOneSuccess {
				cmds = append(cmds, tea.Println("all operations failed"))
			}
			return tea.Sequence(cmds...)
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
	if d.dryrun {
		lines = append(lines, fmt.Sprintf("Faux-deleting %v:", english.Plural(len(selected), d.itemSingular, "")))
	} else {
		lines = append(lines, fmt.Sprintf("Deleting %v:", english.Plural(len(selected), d.itemSingular, "")))
	}
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
	d.msl = multiselectlist.Model[I]{}
	d.confirm = confirmation.Model{}
	return nil
}
