/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package scaffoldselect provides a scaffold for creating actions that allow a user to operate on a list of items.
// While mildly more obtuse than the other scaffolds, scaffoldselect can be used for any action that can easily be applied en-masse.
//
// For example: locking/unlocking user accounts.
package scaffoldselect

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/dustin/go-humanize/english"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CollectItemsFunc is used in interactive mode to populate the list of selectable items.
// It will NOT be called if DirectInvoked or NoInteractive.
//
// ! addtlFlags will be nil if you do not define an addtlFlagFunc in Options.
type CollectItemsFunc[ID_t scaffold.Id_t] func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[ID_t], error)

// OperateFunc performs the actual operation (toggling, cloning, updating, etc) on the final set of selected IDs.
//
// - IDs is the set of identifiers the user provided as bare arguments or interactively selected from the items in CollectItems.
// As a user can bypass CollectItems by providing bare arguments, these IDs are not guaranteed to be valid.
//
// ! addtlFlags will be nil if you do not define an addtlFlagFunc in Options.
//
// results will be printed as they are given, in the order given.
// You may return one result per ID (in which case, the ID should be included in output) or a single result;
// scaffoldselect does no further processing of this output aside from printing to stderr or stdout (if non-interactive).
// You may return no results and a nil error; scaffoldselect will simply exit silently.
//
// an error should be used for a fatal error where processing cannot continue; invalid arguments and non-fatal errors can go in results.
type OperateFunc[ID_t scaffold.Id_t] func(IDs []ID_t, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error)

func NewSelectAction[ID_t scaffold.Id_t](short, long string,
	selectedItemSingular string,
	collectItems CollectItemsFunc[ID_t],
	op OperateFunc[ID_t],
	options Options) action.Pair {
	if collectItems == nil {
		panic("collectItems cannot be nil")
	} else if op == nil {
		panic("operator cannot be nil")
	}
	runE := func(cmd *cobra.Command, args []string) error {
		if options.ValidateArgs != nil {
			inv, err := options.ValidateArgs(cmd.Flags())
			if err != nil {
				return err
			} else if inv != "" {
				return errors.New(inv)
			}
		}

		// if no arguments were given, boot mother or fail out
		if cmd.Flags().NArg() == 0 {
			x, err := cmd.Flags().GetBool(ft.NoInteractive.Name())
			if err != nil {
				clilog.GetFlag(err)
				x = true // better we assume no-interactive
			}
			if !x {
				return mother.Spawn(cmd.Root(), cmd, args)
			}
			if options.Exactly1 {
				return errors.New(phrases.Exactly1ArgRequired(selectedItemSingular))
			}
			return errors.New(phrases.AtLeast1ArgRequired(english.PluralWord(0, selectedItemSingular, "")))
		} else if cmd.Flags().NArg() > 1 && options.Exactly1 {
			return errors.New(phrases.Exactly1ArgRequired(selectedItemSingular))
		}

		results, err := autonomous(cmd.Flags(), op, selectedItemSingular)
		if err != nil {
			return err
		}
		var numSuccesses, numErrors uint
		for _, res := range results {
			if res.Success {
				numSuccesses += 1
				fmt.Fprintln(cmd.OutOrStdout(), res.Output)
			} else {
				numErrors += 1
				fmt.Fprintln(cmd.ErrOrStderr(), res.Output)
			}
		}
		return finalError(numSuccesses, numErrors)
	}

	// generate usage based on given options
	var usage strings.Builder
	if options.AddtlFlags != nil && options.AddtlFlags().NArg() > 0 {
		usage.WriteString(ft.Optional("flags"))
		usage.WriteString(" ")
	}
	if options.Exactly1 {
		usage.WriteString(ft.Mandatory(selectedItemSingular))
	} else {
		usage.WriteString(ft.VariadicArgs(selectedItemSingular, true))
	}

	cmd := treeutils.GenerateAction("select", short, long, runE)
	options.Apply(cmd)

	model := &selectModel[ID_t]{
		singular:     selectedItemSingular,
		collectItems: collectItems,
		op:           op,

		options: options,
	}
	model.Reset()
	return action.NewPair(cmd, model)
}

// callable when enough information was given via args.
// Assumes NArg has already been checked.
// Every result will have out set; successful operations with no success string are not returned.
//
// Returns iff any of the arguments fail the FromString conversion.
func autonomous[ID_t scaffold.Id_t](fs *pflag.FlagSet, op OperateFunc[ID_t], singular string) (_ []scaffold.Result, fatal error) {
	var casts = make([]ID_t, fs.NArg())
	for i, a := range fs.Args() {
		cast, err := scaffold.FromString[ID_t](a)
		if err != nil {
			var zero ID_t
			clilog.Writer.Info("failed to parse string into generic type",
				log.KV("string", a),
				log.KV("target type", reflect.TypeOf(zero)),
				scaffold.IdentifyCaller(),
			)
			return nil, errors.New(a + " is not a valid " + singular)
		}
		casts[i] = cast
	}
	results, err := op(casts, fs)
	if err != nil {
		return nil, err
	}

	return slices.Clip(results), nil
}

// finalError returns an error based on the number of errors.
// Returns nil if numErrors == 0.
func finalError(numSuccesses, numErrors uint) error {
	if numErrors > 0 {
		if numSuccesses == 0 {
			return errors.New("all operations failed")
		}
		return errors.New("some operations failed")
	}
	return nil
}

//#region interactive

type selectModel[ID_t scaffold.Id_t] struct {
	done     bool
	singular string

	collectItems CollectItemsFunc[ID_t]
	op           OperateFunc[ID_t]

	msl multiselectlist.Model[ID_t] // used iff !options.Exactly1
	l   list.Model                  // used iff options.Exactly1

	fs *pflag.FlagSet // scaffoldselect has no flags of its own, so this is just the additional flagset (if provided).

	options Options
}

func (m *selectModel[ID_t]) SetArgs(_ *pflag.FlagSet, args []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	m.fs = &pflag.FlagSet{}
	if m.options.AddtlFlags != nil {
		m.fs = m.options.AddtlFlags()
	}
	if err := m.fs.Parse(args); err != nil {
		return err.Error(), nil, nil
	}

	if m.options.ValidateArgs != nil {
		if inv, err := m.options.ValidateArgs(m.fs); err != nil {
			return "", nil, err
		} else if inv != "" {
			return inv, nil, err
		}
	}

	if m.fs.NArg() > 0 { // we should be able to operate autonomously
		if m.fs.NArg() > 1 && m.options.Exactly1 {
			return phrases.Exactly1ArgRequired(m.singular), nil, nil
		}
		results, err := autonomous(m.fs, m.op, m.singular)
		if err != nil {
			return "", nil, err
		}

		m.done = true
		return "", teaPrintlnResults(results), nil
	}

	// we were not given any arguments; spool up the selection list

	itms, err := m.collectItems(m.fs)
	if err != nil {
		return "", nil, err
	} else if len(itms) < 1 {
		err = errors.New("You have no available " + english.PluralWord(0, m.singular, ""))
		if m.options.NoItemsError != nil {
			err = errors.New(m.options.NoItemsError(m.fs))
		}
		return "", nil, err
	}

	if m.options.Exactly1 {
		// need to re-wrap the items as list.Items as Go cannot duck type arrays of interfaces
		wrapped := make([]list.Item, len(itms))
		for i, itm := range itms {
			wrapped[i] = itm
		}
		m.l = stylesheet.NewList(wrapped, width, height, m.singular, english.PluralWord(len(itms), m.singular, ""))
	} else {
		m.msl = multiselectlist.Model[ID_t](multiselectlist.New(itms, width, height, multiselectlist.Options{}))
	}

	return "", nil, nil
}

// teaPrintlnResults returns a set of tea Cmds to print the results (color-coded) and suffixes finalError
func teaPrintlnResults(results []scaffold.Result) tea.Cmd {
	if len(results) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, len(results))
	var numSuccesses, numErrors uint
	for i, res := range results {
		if res.Success {
			numSuccesses += 1
			cmds[i] = tea.Println(res.Output)
		} else {
			numErrors += 1
			cmds[i] = tea.Println(stylesheet.Cur.ErrorText.Render(res.Output))
		}
	}
	if err := finalError(numSuccesses, numErrors); err != nil {
		cmds = append(cmds, tea.Println(err.Error()))
	}
	return tea.Sequence(cmds...)
}

func (m *selectModel[ID_t]) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	if m.options.Exactly1 {
		if hotkeys.ButtonPressed(msg) { // accept invoke or select
			m.done = true
			// collect the set of IDs
			itm := m.l.SelectedItem()
			if itm == nil {
				clilog.Writer.Error("nil item selected from list.Model")
				return tea.Println(clilog.ErrInternal{}.Error())
			}
			selItm, ok := itm.(multiselectlist.SelectableItem[ID_t])
			if !ok {
				var zero multiselectlist.SelectableItem[ID_t]
				return tea.Println(clilog.TypeAssert(itm, zero).Error())
			}
			results, err := m.op([]ID_t{selItm.ID()}, m.fs)
			if err != nil {
				return tea.Println(err.Error())
			}
			return teaPrintlnResults(results)
		}

		m.l, cmd = m.l.Update(msg)
		return cmd
	}

	// multiselect branch

	m.msl, cmd = m.msl.Update(msg)
	if !m.msl.Done() {
		return cmd
	}
	m.done = true

	itms := m.msl.GetSelectedItems()
	if len(itms) == 0 {
		m.msl.Undone()
		return m.msl.NewStatusMessage("select at least 1 " + m.singular)
	}
	IDs := make([]ID_t, len(itms))
	// collect IDs
	for i, itm := range itms {
		IDs[i] = itm.ID()
	}
	results, err := m.op(IDs, m.fs)
	if err != nil {
		return tea.Println(stylesheet.Cur.ErrorText.Render(err.Error()))
	}
	return teaPrintlnResults(results)
}

func (m *selectModel[ID_t]) View() string {
	if m.options.Exactly1 {
		return m.l.View()
	}
	return m.msl.View()
}

func (m *selectModel[ID_t]) Done() bool {
	return m.done
}

func (m *selectModel[ID_t]) Reset() error {
	m.done = false
	m.msl = multiselectlist.Model[ID_t]{}
	m.l = list.Model{}
	return nil
}
