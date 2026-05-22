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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CollectItemsFunc is used in interactive mode to populate the list of selectable items.
//
// ! addtlFlags will be nil if you do not define an addtlFlagFunc in Options.
type CollectItemsFunc[ID_t scaffold.Id_t] func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[ID_t], error)

// OperateFunc performs the actual operation (toggling, cloning, updating, etc) on a given ID.
//
// ! addtlFlags will be nil if you do not define an addtlFlagFunc in Options.
type OperateFunc[ID_t scaffold.Id_t] func(id ID_t, addtlFlags *pflag.FlagSet) (success string, _ error)

func NewSelectAction[ID_t scaffold.Id_t](short, long string,
	singular, plural string,
	collectItems CollectItemsFunc[ID_t],
	op OperateFunc[ID_t],
	options Options) action.Pair {
	if collectItems == nil {
		panic("collectItems cannot be nil")
	} else if op == nil {
		panic("operator cannot be nil")
	}
	runE := func(cmd *cobra.Command, args []string) error {
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
			return errors.New(phrases.AtLeast1ArgRequired(plural))
		}

		atLeastOneSuccess := false
		for _, a := range cmd.Flags().Args() {
			cast, err := scaffold.FromString[ID_t](a)
			if err != nil {
				var zero ID_t
				clilog.Writer.Info("failed to parse string into generic type",
					log.KV("string", a),
					log.KV("target type", reflect.TypeOf(zero)),
					scaffold.IdentifyCaller(),
				)
				return errors.New(a + " is not a valid " + singular)
			}

			// we don't have a great way of passing in just the additional flags, so the non-interactive version gets all flags.
			if success, err := op(cast, cmd.Flags()); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
			} else {
				atLeastOneSuccess = true
				if success != "" {
					fmt.Fprintln(cmd.OutOrStdout(), success)
				}
			}
		}
		if !atLeastOneSuccess {
			return errors.New("all operations failed")
		}
		return nil
	}
	cmd := treeutils.GenerateAction("select", short, long, nil, runE,
		treeutils.GenerateActionOptions{
			Usage: ft.VariadicArgs(singular, true),
		})
	options.Apply(cmd)

	model := &selectModel[ID_t]{
		singular:     singular,
		plural:       plural,
		collectItems: collectItems,
		op:           op,

		options: options,
	}
	model.Reset()
	return action.NewPair(cmd, model)
}

//#region interactive

type selectModel[ID_t scaffold.Id_t] struct {
	singular, plural string

	collectItems CollectItemsFunc[ID_t]
	op           OperateFunc[ID_t]

	msl multiselectlist.Model[ID_t]

	fs *pflag.FlagSet // scaffoldselect has no flags of its own, so this is just the additional flagset (if provided).

	options Options
}

func (m *selectModel[ID_t]) SetArgs(_ *pflag.FlagSet, args []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	if m.options.AddtlFlags != nil {
		m.fs = m.options.AddtlFlags()
		if err := m.fs.Parse(args); err != nil {
			return err.Error(), nil, nil
		}
	}

	itms, err := m.collectItems(m.fs)
	if err != nil {
		return "", nil, err
	} else if len(itms) < 1 {
		err = errors.New("You have no available " + m.plural)
		if m.options.NoItemsError != "" {
			err = errors.New(m.options.NoItemsError)
		}
		return "", nil, err
	}
	m.msl = multiselectlist.Model[ID_t](multiselectlist.New(itms, width, height, multiselectlist.Options{}))

	return "", nil, nil
}

func (m *selectModel[ID_t]) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.msl, cmd = m.msl.Update(msg)
	if !m.msl.Done() {
		return cmd
	}

	itms := m.msl.GetSelectedItems()
	if len(itms) == 0 {
		m.msl.Undone()
		return m.msl.NewStatusMessage("select at least 1 " + m.singular)
	}
	var cmds = make([]tea.Cmd, 0, len(itms))
	atLeastOneSuccess := false
	for _, itm := range itms {
		if success, err := m.op(itm.ID(), m.fs); err != nil {
			cmds = append(cmds, tea.Println(err))
		} else {
			atLeastOneSuccess = true
			if success != "" {
				cmds = append(cmds, tea.Println(success))
			}
		}
	}
	cmds = slices.Clip(cmds)
	if !atLeastOneSuccess {
		cmds = append(cmds, tea.Println("all operations failed"))
	}

	return tea.Sequence(cmds...)
}

func (m *selectModel[ID_t]) View() string {
	return m.msl.View()
}

func (m *selectModel[ID_t]) Done() bool {
	return m.msl.Done()
}

func (m *selectModel[ID_t]) Reset() error {
	m.msl = multiselectlist.Model[ID_t]{}
	return nil
}
