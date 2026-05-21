package scaffoldselect

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewSelectAction[ID_t comparable](short, long string,
	singular, plural string,
	collectItems func() ([]multiselectlist.SelectableItem[ID_t], error),
	op func(id ID_t) error,
	fromString func(s string) (cast ID_t, invalid string),
	options Options) action.Pair {
	if collectItems == nil {
		panic("collectItems cannot be nil")
	} else if op == nil {
		panic("operator cannot be nil")
	} else if fromString == nil {
		panic("fromString cannot be nil")
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

		// at least one ID was specified, test each arg
		for _, a := range cmd.Flags().Args() {
			cast, invalid := fromString(a)
			if invalid != "" {
				return errors.New(invalid)
			}
			err := op(cast)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
			}
			if options.SuccessString != nil {
				success := options.SuccessString(cast)
				if success != "" {
					fmt.Fprintln(cmd.OutOrStdout(), success)
				}
			}
		}
		return nil
	}
	cmd := treeutils.GenerateAction("select", short, long, nil, runE,
		treeutils.GenerateActionOptions{
			Usage: ft.VariadicArgs(singular, true),
		})

	model := &selectModel[ID_t]{
		singular:     singular,
		collectItems: collectItems,
		op:           op,

		options: options,
	}
	model.Reset()
	return action.NewPair(cmd, model)
}

//#region interactive

type selectModel[ID_t comparable] struct {
	singular string

	collectItems func() ([]multiselectlist.SelectableItem[ID_t], error)
	op           func(ID_t) error

	msl multiselectlist.Model[ID_t]

	options Options
}

func (m *selectModel[ID_t]) SetArgs(_ *pflag.FlagSet, args []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	itms, err := m.collectItems()
	if err != nil {
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
	var cmds = make([]tea.Cmd, len(itms))
	var succeededAtLeastOnce bool
	for i, itm := range itms {
		if err := m.op(itm.ID()); err != nil {
			cmds[i] = tea.Println(err)
		} else {
			succeededAtLeastOnce = true
			if m.options.SuccessString != nil {
				if success := m.options.SuccessString(itm.ID()); success != "" {
					cmds[i] = tea.Println(success)
				}
			}
		}
	}
	if !succeededAtLeastOnce {
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
