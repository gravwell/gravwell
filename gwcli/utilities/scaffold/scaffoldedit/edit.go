/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package scaffoldedit provides a template for building actions that modify existing data.

An edit action allows the user to select an entity from a list of all available entities, modify its
fields, and reflect the changes to the server.

Implementor (you) must provide a struct of subroutines and a map of manipulate-able Fields to be displayed
after an item is selected for editing.
The subroutines provide methods for scaffoldedit to find and manipulate data,
including translation services for plucking specific fields out of the generic data struct.

This scaffold is notably more complex to modify and heavier to implement than the other scaffolds.
See the Design block below for why.

! Once a Config is given by the implementor, it should be considered ReadOnly.

! Note that some subs in the SubroutineSet explicitly pass pointers as parameters; these subroutines
are destructive by design.

Implementations will resemble scaffoldcreate implementations with the addition of a SubroutineSet.
An example implementation doesn't really make sense due to the amount that scaffoldedit requires from the
implementor; instead take a look at the macro edit action implementation. That is fairly simple.
*/
package scaffoldedit

/**
 * More on Design:
 * Edit is definitely the most complex of the scaffolds, requiring components of both Create
 * (arbitrary TIs) and Delete (list possible structs/items).
 * By virtue of passing around structs and ids, it was always going to require multiple generics.
 * As implemented, it uses I to represent a singular, generally-numeric ID and S to represent a
 * single instance of the struct we are/will be editing.
 * The use of reflection to reduce the complexity of the SubroutineSet, thereby reducing implementor
 * load, was considered, but ditched fairly early.
 * Reflection is
 * 1) slow
 * 2) error-prone (needing to look up qualified field names given by the implementor)
 * 3) an added layer of complexity on top of the already-in-play generics
 * Thus, no reflection.
 * The side effect of this, of course, is that we need yet more functions from the implementor and a
 * couple of trivial get/sets to be able to operate on the struct we want to update.
 *
 * Not sharing the Field struct between edit and create was a conscious choice to allow them to be
 * updated independently as it is more coincidental that they are similar.
 */

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	listHeightMax  = 40 // lines
	successStringF = "Successfully updated %v %v"
)

// NewEditAction composes a usable edit action, returning its action pair.
// The parameters, specifically funcs, do most of the heavy lifting; this just bolts on necessities to make the new action work in Mother and via a script.
// This is the function that implementations/implementors should call as their action implementation.
// This function panics if any parameters are missing.
func NewEditAction[I scaffold.Id_t, S any](singular, plural string, cfg Config, funcs SubroutineSet[I, S]) action.Pair {
	funcs.guarantee() // check that all functions are given
	if len(cfg) < 1 { // check that config has fields in it
		panic("cannot edit with no fields defined")
	}
	if strings.TrimSpace(singular) == "" {
		panic("singular form of the noun cannot be empty")
	} else if strings.TrimSpace(plural) == "" {
		panic("plural form of the noun cannot be empty")
	}

	var fs = generateFlagSet(cfg, singular)

	cmd := treeutils.GenerateAction(
		"edit",                             // use
		"edit a "+singular,                 // short
		"edit/alter an existing "+singular, // long
		[]string{"e"},                      // aliases
		func(cmd *cobra.Command, args []string) {
			var err error
			// hard branch on noInteractive mode
			var noInteractive bool
			if noInteractive, err = cmd.Flags().GetBool(ft.NoInteractive.Name); err != nil {
				clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
				return
			}
			if noInteractive {
				runNonInteractive(cmd, cfg, funcs, singular)
			} else {
				runInteractive(cmd, args)
			}
		})

	// attach flags to cmd
	cmd.Flags().AddFlagSet(&fs)

	return action.NewPair(cmd,
		newEditModel(cfg, singular, plural, funcs, fs),
	)
}

// Generates a flagset from the given configuration and appends flags native to scaffoldedit.
func generateFlagSet(cfg Config, singular string) pflag.FlagSet {
	var fs pflag.FlagSet
	for _, field := range cfg {
		if field.FlagName == "" {
			field.FlagName = ft.DeriveFlagName(field.Title)
		}

		// map fields to their flags
		if field.FlagShorthand != 0 {
			fs.StringP(field.FlagName, string(field.FlagShorthand), "", field.Usage)
		} else {
			fs.String(field.FlagName, "", field.Usage)
		}
	}

	// attach native flags
	fs.StringP(ft.Name.ID, "i", "", fmt.Sprintf("id of the %v to edit", singular))

	return fs
}

// run helper function.
// runNonInteractive is the --no-interactive portion of edit's runFunc.
// It requires --id be set and is ineffectual if no other flags were given.
// Prints and error handles on its own; the program is expected to exit on its completion.
func runNonInteractive[I scaffold.Id_t, S any](cmd *cobra.Command, cfg Config, funcs SubroutineSet[I, S], singular string) {
	var err error
	var (
		id   I
		zero I
		itm  S
	)
	if strid, err := cmd.Flags().GetString(ft.Name.ID); err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		return
	} else {
		id, err = scaffold.FromString[I](strid)
		if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
			return
		}
	}
	if id == zero { // id was not given
		fmt.Fprintln(cmd.OutOrStdout(), "--"+ft.Name.ID+" is required in no-interactive mode")
		return
	}

	// get the item to edit
	if itm, err = funcs.SelectSub(id); err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(),
			fmt.Sprintf("Failed to select %s (id: %v): %s\n",
				singular, id, err.Error()))
		return
	}

	var fieldUpdated bool   // was a value actually changed?
	for k, v := range cfg { // check each field for updates to be made
		// get current value
		curVal, err := funcs.GetFieldSub(itm, k)
		if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
			return
		}
		var newVal = curVal
		if cmd.Flags().Changed(v.FlagName) { // flag *presumably* updates the field
			if x, err := cmd.Flags().GetString(v.FlagName); err != nil {
				clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
				return
			} else {
				newVal = x
			}
		}

		if newVal != curVal { // update the struct
			fieldUpdated = true // note if a change occurred
			if inv, err := funcs.SetFieldSub(&itm, k, newVal); err != nil {
				clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
				return
			} else if inv != "" {
				fmt.Fprintln(cmd.OutOrStdout(), inv)
				return
			}
		}
	}

	if !fieldUpdated { // only bother to update if at least one field was changed
		clilog.Tee(clilog.INFO, cmd.OutOrStdout(), "no field would be updated; quitting...\n")
		return
	}

	// perform the actual update
	identifier, err := funcs.UpdateSub(&itm)
	if err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), successStringF+"\n", singular, identifier)

}

// run helper function.
// Boots Mother, allowing her to handle the request.
func runInteractive(cmd *cobra.Command, args []string) {
	// we have no way of knowing if the user has passed enough data to make the edit autonomously
	// ex: they provided one flag, but are they only planning to edit one flag?
	// therefore, just spawn mother; she is smart enough to handle the flags naturally
	if err := mother.Spawn(cmd.Root(), cmd, args); err != nil {
		clilog.Writer.Critical(err.Error())
	}
}

//#region interactive mode (model) implementation

// the possible modes editModel can be in
type mode = uint8

const (
	quitting  mode = iota // mother should reassert
	selecting             // picking from a list of edit-able items
	editing               // item selected; currently altering
	idle                  // inactive
)

type editModel[I scaffold.Id_t, S any] struct {
	mode             mode                // current program state
	fs               pflag.FlagSet       // current state of the flagset
	singular, plural string              // forms of the noun
	width, height    int                 // tty dimensions, queried by SetArgs()
	funcs            SubroutineSet[I, S] // functions provided by implementor

	cfg Config // RO configuration provided by the caller

	data []S // full, raw data retrieved by fchFunc

	list            list.Model // list displayed during `selecting` mode
	listInitialized bool       // check before accessing the list, in case the user skipped to edit mode

	// editing-specific fields
	editing   stateEdit[S]
	updateErr string // error occurred performing the update
}

// Creates and returns a new edit model, ready for interactive use.
func newEditModel[I scaffold.Id_t, S any](cfg Config, singular, plural string,
	funcs SubroutineSet[I, S], initialFS pflag.FlagSet) *editModel[I, S] {
	em := &editModel[I, S]{
		mode:     idle,
		fs:       initialFS,
		singular: singular,
		plural:   plural,
		cfg:      cfg,
		funcs:    funcs,
		editing:  stateEdit[S]{},
	}

	return em
}

func (em *editModel[I, S]) SetArgs(_ *pflag.FlagSet, tokens []string) (
	invalid string, onStart tea.Cmd, err error,
) {
	// parse the flags, save them for later, when TIs are created
	if err := em.fs.Parse(tokens); err != nil {
		return err.Error(), nil, nil
	}

	// check for an explicit ID
	if em.fs.Changed(ft.Name.ID) {
		var id I
		if strid, err := em.fs.GetString(ft.Name.ID); err != nil {
			return "", nil, err
		} else {
			id, err = scaffold.FromString[I](strid)
			if err != nil {
				return "failed to parse id from " + strid, nil, nil
			}
		}

		// select the item associated to the id
		item, err := em.funcs.SelectSub(id)
		if err != nil {
			// treat this as an invalid argument
			return fmt.Sprintf("failed to fetch %s by id (%v): %v", em.singular, id, err), nil, nil
		}
		// we can jump directly to editing phase on start
		if err := em.enterEditMode(item); err != nil {
			em.mode = quitting
			clilog.Writer.Errorf("%v", err)
			return "", nil, err
		}

		return "", nil, nil

	}

	// fetch edit-able items
	if em.data, err = em.funcs.FetchSub(); err != nil {
		return
	}

	var dataCount = len(em.data)

	// check for a lack of data
	if dataCount < 1 { // die
		em.mode = quitting
		return "", tea.Printf("You have no %v that can be edited", em.plural), nil
	}

	// transmute data into list items
	var itms = make([]list.Item, dataCount)
	for i, s := range em.data {
		itms[i] = item{em.funcs.GetTitleSub(s), em.funcs.GetDescriptionSub(s)}
	}

	// generate list
	em.list = stylesheet.NewList(itms, 80, listHeightMax, em.singular, em.plural)
	em.listInitialized = true
	em.mode = selecting

	return "", nil, nil
}

func (em *editModel[I, S]) Update(msg tea.Msg) tea.Cmd {
	if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
		em.width = wsMsg.Width
		em.height = wsMsg.Height
		// if we skipped directly to edit mode, list will be nil
		if em.listInitialized {
			em.list.SetHeight(min(wsMsg.Height-2, listHeightMax))
		}
	} else if _, ok := msg.(tea.KeyMsg); ok {
		em.updateErr = ""
	}

	// switch handling based on mode
	switch em.mode {
	case quitting:
		return nil
	case selecting:
		return em.updateSelecting(msg)
	case editing:
		cmd, identifier := em.editing.update(msg, em.cfg, em.funcs.SetFieldSub, em.funcs.UpdateSub)
		if identifier != "" {
			em.mode = quitting
			return tea.Printf(successStringF, em.singular, identifier)
		}
		return cmd
	default:
		clilog.Writer.Criticalf("unknown edit mode %v.", em.mode)
		clilog.Writer.Debugf("model dump: %#v.", em)
		clilog.Writer.Info("Returning control to Mother...")
		em.mode = quitting
		return textinput.Blink
	}
}

// Update() handling for selecting mode.
// Updates the list and transitions to editing mode if an item is selected.
func (em *editModel[I, S]) updateSelecting(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeySpace || msg.Type == tea.KeyEnter {
			item := em.data[em.list.Index()]
			if err := em.enterEditMode(item); err != nil {
				em.mode = quitting
				clilog.Writer.Errorf("%v", err)
				return tea.Println(err.Error())
			}
			return textinput.Blink
		}
	}
	var cmd tea.Cmd
	em.list, cmd = em.list.Update(msg)
	return cmd
}

func (em *editModel[I, S]) View() string {
	switch em.mode {
	case quitting:
		return ""
	case selecting:
		return em.list.View() + "\n" +
			stylesheet.Cur.ExampleText.
				AlignHorizontal(lipgloss.Center).
				Width(em.width).
				Render("Press space or enter to select")
	case editing:
		return em.editing.view()
	default:
		clilog.Writer.Errorf("unknown mode %v", em.mode)
		em.mode = quitting
		return ""
	}
}

func (em *editModel[I, S]) Done() bool {
	return em.mode == quitting
}

func (em *editModel[I, S]) Reset() error {
	em.mode = idle
	em.data = nil
	em.fs = generateFlagSet(em.cfg, em.singular)

	// selecting mode
	em.list = list.Model{}
	em.listInitialized = false

	// editing mode
	em.editing.reset()
	em.updateErr = ""

	return nil
}

// Triggers the edit model to enter editing mode, establishing and displaying a TI for each field
// and sorting them into an ordered array.
func (em *editModel[I, S]) enterEditMode(item S) error {
	es := stateEdit[S]{
		item:        item,
		tiCount:     len(em.cfg),
		orderedKTIs: make([]scaffold.KeyedTI, len(em.cfg)),
	}

	// use the get function to pull current values for each field and display them in their
	// respective TIs
	var i uint8 = 0
	for k, fieldCfg := range em.cfg {
		// create the ti
		var ti textinput.Model
		if fieldCfg.CustomTIFuncInit != nil {
			ti = fieldCfg.CustomTIFuncInit()
		} else {
			ti = stylesheet.NewTI("", !fieldCfg.Required)
		}

		var setByFlag bool
		if em.fs.Changed(fieldCfg.FlagName) { // prefer flag value
			if x, err := em.fs.GetString(fieldCfg.FlagName); err == nil {
				ti.SetValue(x)
				setByFlag = true
			}
		}

		if !setByFlag { // fallback to current value
			curVal, err := em.funcs.GetFieldSub(es.item, k)
			if err != nil {
				return err
			}
			ti.SetValue(curVal)
		}

		// attach TI to list
		es.orderedKTIs[i] = scaffold.KeyedTI{
			Key:        k,
			FieldTitle: fieldCfg.Title,
			TI:         ti,
			Required:   fieldCfg.Required}
		i += 1

		// check width
		es.longestWidth = max(lipgloss.Width(fieldCfg.Title)+3+ti.Width, es.longestWidth)
	}

	if len(es.orderedKTIs) < 1 {
		return errors.New("no TIs created by transmutation")
	}

	// order TIs from highest to lowest orders
	slices.SortFunc(es.orderedKTIs, func(a, b scaffold.KeyedTI) int {
		return em.cfg[b.Key].Order - em.cfg[a.Key].Order
	})

	es.orderedKTIs[0].TI.Focus() // focus the first TI

	em.editing = es
	em.mode = editing
	return nil
}

//#endregion interactive mode (model) implementation

type item struct {
	title       string
	description string
}

var _ stylesheet.ListItem = item{}

func (i item) Title() string {
	return i.title
}

func (i item) Description() string {
	return i.description
}

func (i item) FilterValue() string {
	return i.title
}
