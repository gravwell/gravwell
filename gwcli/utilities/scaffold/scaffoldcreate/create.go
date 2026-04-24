/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package scaffoldcreate provides a template for building actions that create new data or configuration.

A create action creates a shallow list of inputs for the user to fill via flags or interactive
TIs before being passed back to the progenitor to transform into usable data for their create
function.

The available fields are fairly configurable, the progenitor provides their own map of Field
structs, and easily extensible, the struct can have more options or formats bolted on without too
much trouble.

This scaffold is a bit easier to extend than Delete and List, given it did not require generics.

Look to the scheduled query creation action (external to the one built into DataScope) or macro
creation action as two examples of implementation styles.

! Once a Config is given by the caller, it should be considered ReadOnly.

NOTE: More complex creation with nested options and mutli-stage flows should be built
independently. This scaffold is intended for simple, handful-of-field creations.

Example implementation:

	func NewCreateAction() action.Pair {
		n := scaffoldcreate.NewField(true, "name", 100)
		d := scaffoldcreate.NewField(true, "value", 90)
		fields := scaffoldcreate.Config{
			"name":  n,
			"value": d,
			"field3": scaffoldcreate.Field{
				Required:      true,
				Title:         "field3",
				Usage:         "field 3 usage",
				Type:          scaffoldcreate.Text,
				FlagName:      "flagn",
				FlagShorthand: 'f',
				DefaultValue:  "",
				TI: struct {
					Order       int
					Placeholder string
					Validator   func(s string) error
				}{
					Order: 80,
				},
			},
		}

		return scaffoldcreate.NewCreateAction("", fields, create)
	}

	func create(_ scaffoldcreate.Config, vals scaffoldcreate.Values) (any, string, error) {
		id, err := connection.Client.X()
		return id, "", err
	}
*/
package scaffoldcreate

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	errMissingRequiredFlags string = "missing required flags %v"
	createdSuccessfully     string = "Successfully created %v (ID: %v)."
)

// A Config maps keys -> Field; used as (ReadOnly) configuration for this creation instance
type Config = map[string]Field

// CreateFuncT defines the format of the subroutine that must be passed for creating data.
// The function's return values must be:
//
// the id of the newly created value (likely as returned by the Gravwell backend)
//
// a reason the create attempt was invalid (or the empty string)
//
// and an error that occurred (or nil). This is different than an invalid reason and is likely a bubbling up of an error from the client library.
type CreateFuncT func(fields Config, fs *pflag.FlagSet) (id any, invalid string, err error)

// NewCreateAction returns an action pair (covering interactive and non-interactive use) capable of creating new data based on user input.
// You must tell the create action what kind of data it accepts (in the form of fields) and
// what function to pass the populated fields to in order to actually *create* the thing (in the form of a CreateFunc).
//
// Singular is the singular version of the noun you are creating. Ex: "macro", "resource", "query".
func NewCreateAction(singular string, fields Config, createFunc CreateFuncT, opts Options) action.Pair {
	// nil check singular
	if singular == "" {
		clilog.Writer.Error("singular noun cannot be empty. Defaulting to \"UNKNOWN\"", scaffold.IdentifyCaller())
		singular = "UNKNOWN"
	}

	// check that every field has a provider
	for key := range fields {
		if fields[key].Provider == nil {
			clilog.Writer.Error("field is missing a provider", attachLogInfo(key)...)
			delete(fields, key)
		}
	}

	// pull flags from provided fields
	var flags = installFlagsFromFields(fields)
	if opts.AddtlFlags != nil {
		afs := opts.AddtlFlags()
		flags.AddFlagSet(&afs)
	}

	// pull required flags from cfg to set usage
	requiredFlags := make([]string, 0)
	for _, v := range fields {
		if v.Required && v.Flag.Name != "" {
			txt := "--" + v.Flag.Name + "=" + ft.Mandatory("string")
			requiredFlags = append(requiredFlags, txt)
		}
	}

	cmd := treeutils.GenerateAction(
		"create",                 // use
		"create a "+singular,     // short
		"create a new "+singular, // long
		[]string{},               // aliases
		func(c *cobra.Command, s []string) {
			// check non-interactive
			noInteractive, err := c.Flags().GetBool(ft.NoInteractive.Name())
			if err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error()+"\n")
				return
			}
			// check and set flags; spool up mother to prompt for missing required flags if !non-interactive
			if mr, err := setValuesFromFlags(c.Flags(), fields); err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error()+"\n")
				return
			} else if mr != nil {
				if !noInteractive {
					if err := mother.Spawn(c.Root(), c, s); err != nil {
						clilog.Writer.Critical(err.Error())
					}
					return
				} else {
					fmt.Fprintf(c.OutOrStdout(), errMissingRequiredFlags+"\n", mr)
				}
				return
			}

			// if all files were valid and we aren't missing any requires, we can attempt to jump directly into creation.
			// gather all values to pass to the create func

			// attempt to create the new X
			if id, inv, err := createFunc(fields, c.Flags()); err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error()+"\n")
				return
			} else if inv != "" { // some of the flags were invalid
				fmt.Fprintln(c.OutOrStdout(), inv)
				return
			} else {
				fmt.Fprintf(c.OutOrStdout(), createdSuccessfully, singular, id)
			}
		}, treeutils.GenerateActionOptions{Usage: strings.Join(requiredFlags, " ")})

	// apply options
	if opts.Use != "" {
		cmd.Use = opts.Use
	}
	if len(opts.Aliases) > 0 {
		cmd.Aliases = opts.Aliases
	}

	// attach mined flags to cmd
	cmd.Flags().AddFlagSet(&flags)

	// initialize every field
	for key := range fields {
		fields[key].Provider.Initialize(fields[key].DefaultValue, fields[key].Required)
	}

	return action.NewPair(cmd, newCreateModel(fields, singular, createFunc, opts))
}

//#region interactive mode (model) implementation

const defaultWidth = 80 // default wrap width, used before initial WinMsgSz arrives

type mode uint // state of the interactive application

const (
	inputting mode = iota // user entering data
	quitting              // done
)

type inputs struct {
	selected uint     // currently focused item (index correlates to "ordered"+1 (submit))
	err      string   // a reason inputs are invalid. Currently only holds the most-recently set error. Disables submit if set.
	ordered  []string // list of keys, ordered at create-time by Config.Field.Order
	takeover string   // key of the field currently running the show
}

// interactive model that builds out inputs based on the read-only Config supplied on creation.
type createModel struct {
	mode mode

	width int // tty width

	singular string // "macro", "search", etc

	fields Config // RO configuration provided by the caller

	inputs             inputs
	longestTitleLength int // max len(field.Title) across all fields; set at create time for title alignment

	createErr string // the reason the last create failed (not for invalid parameters)

	// function to provide additional flags for this specific create instance
	addtlFlagFunc func() pflag.FlagSet
	// current state of the flagset, Reset to addtlFlagFunc + installFlags
	fs pflag.FlagSet
	cf CreateFuncT // function to create the new entity
}

// SubmitSelect returns if the select button is currently selected by the user.
func (c *createModel) SubmitSelected() bool {
	return c.inputs.selected == uint(len(c.inputs.ordered))
}

// Creates and returns a create Model, ready for interactive usage via Mother.
func newCreateModel(fields Config, singular string, createFunc CreateFuncT, opts Options) *createModel {
	c := &createModel{
		mode:     inputting,
		width:    defaultWidth,
		singular: singular,
		fields:   fields,
		inputs: inputs{
			ordered: slices.Collect(maps.Keys(fields)),
		},
		addtlFlagFunc: opts.AddtlFlags,
		cf:            createFunc,
	}

	// set flags by mining fields and, if applicable, tacking on additional flags
	c.fs = installFlagsFromFields(fields)
	if c.addtlFlagFunc != nil {
		addtlFlags := c.addtlFlagFunc()
		c.fs.AddFlagSet(&addtlFlags)
	}

	slices.SortStableFunc(c.inputs.ordered, func(aKey, bKey string) int {
		// sort on order, then alpha on title
		switch {
		case fields[aKey].Order < fields[bKey].Order:
			return 1
		case fields[aKey].Order > fields[bKey].Order:
			return -1
		}
		return strings.Compare(fields[aKey].Title, fields[bKey].Title)
	})

	// compute longestFieldLength for title column alignment in View()
	for _, field := range fields {
		if titleLen := len(field.Title); titleLen > c.longestTitleLength {
			c.longestTitleLength = titleLen
		}
	}

	// focus the first input
	if len(c.inputs.ordered) > 0 {
		c.fields[c.inputs.ordered[0]].Provider.ToggleFocus(true)
	}

	return c
}

// Init is unused. It just exists so we can feed createModel into teatest.
func (c *createModel) Init() tea.Cmd {
	return nil
}

func (c *createModel) Update(msg tea.Msg) tea.Cmd {
	if c.mode == quitting {
		return nil
	} else if c.inputs.takeover != "" {
		cmd, takeover := c.fields[c.inputs.takeover].Provider.Update(true, msg) // takeover mode implies selected
		if !takeover {
			c.inputs.takeover = ""
		}
		return cmd

	}
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		c.width = sizeMsg.Width
		// forward this message to each field
		var cmds []tea.Cmd
		for i, key := range c.inputs.ordered {
			if cmd, _ := c.fields[key].Provider.Update(i == int(c.inputs.selected), msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if len(cmds) > 0 {
			return tea.Batch(cmds...)
		}
		return nil
	} else if hotkeys.IsCursorUp(msg) {
		c.focusPrevious()
		return textinput.Blink
	} else if hotkeys.IsCursorDown(msg) {
		c.focusNext()
		return textinput.Blink
	} else if (hotkeys.IsInvoke(msg) || hotkeys.IsSelect(msg)) && c.SubmitSelected() {
		// double check that all fields are satisfied
		c.checkSatisfaction(false)
		if c.inputs.err != "" {
			return nil
		}
		id, invalid, err := c.cf(c.fields, &c.fs)
		if err != nil {
			c.createErr = err.Error()
			return nil
		} else if invalid != "" {
			c.inputs.err = invalid
			return nil
		}
		// done, die
		c.mode = quitting
		return tea.Println(fmt.Sprintf(createdSuccessfully, c.singular, id))
	}
	if c.SubmitSelected() { // if submit is selected and it wasn't handled above, we don't care about it
		return nil
	}

	// pass the message to the currently selected input

	p := c.selectedField().Provider
	cmd, takeover := p.Update(true, msg)
	if takeover {
		c.inputs.takeover = c.inputs.ordered[c.inputs.selected]
	}
	c.checkSatisfaction(false)
	return cmd
}

func (c *createModel) selectedField() Field {
	return c.fields[c.inputs.ordered[c.inputs.selected]]
}

// sets inputs.err to the reason of the first invalid field, then exits.
// Empties inputs.err out iff every field is satisfied.
//
// If selcetedOnly is set, only the currently selected field is checked.
func (c *createModel) checkSatisfaction(selectedOnly bool) {
	check := func(f Field) bool {
		if f.Required && f.Provider.Get() == "" {
			c.inputs.err = phrases.MissingRequiredField(f.Title)
			return true
		}

		if invalid := f.Provider.Satisfied(); invalid != "" {
			c.inputs.err = invalid
			return true
		}
		c.inputs.err = ""
		return false
	}
	if selectedOnly {
		check(c.selectedField())
		return
	}
	for _, key := range c.inputs.ordered {
		if check(c.fields[key]) {
			return
		}
	}
}

// Blurs the current input, selects and focuses the next one c.inputs.ordered.
func (c *createModel) focusNext() {
	c.focusInput(false)
	c.inputs.selected += 1
	if c.inputs.selected > uint(len(c.inputs.ordered)) { // jump to start
		c.inputs.selected = 0
	}
	c.focusInput(true)
}

// Blurs the current input, selects and focuses the previous one in c.inputs.ordered.
func (c *createModel) focusPrevious() {
	c.focusInput(false)

	if c.inputs.selected == 0 { // wrap to submit button
		c.inputs.selected = uint(len(c.inputs.ordered))
	} else {
		c.inputs.selected -= 1
	}
	c.focusInput(true)
}

// focusInput toggles the focus on the currently selected input (doing nothing if submit is selected).
// If !focus, blurs the input.
func (c *createModel) focusInput(focus bool) {
	if c.SubmitSelected() {
		return
	}
	key := c.inputs.ordered[c.inputs.selected]
	c.fields[key].Provider.ToggleFocus(focus)
}

// View composes each field's view into a set, using the total width of TitleValue views to center Line and second-line views.
func (c *createModel) View() string {
	if c.inputs.takeover != "" {
		_, v, _ := c.fields[c.inputs.takeover].Provider.View(true, c.width)
		return v
	}

	// View operates in two passes:
	// one to collect and measure the width of TitleValue views
	// one to center remaining views and compose the final, returned view.

	views, setWidth := c.collectViewValues()
	if setWidth == 0 { // if we somehow have no TitleValues, use the entire pane
		setWidth = c.width
	}

	// build final lines, centering Line/secondLine entries under modalWidth
	centerSty := lipgloss.NewStyle().Width(setWidth).AlignHorizontal(lipgloss.Center)
	lines := make([]string, 0, len(views))
	for _, v := range views {
		if v.toCenter {
			lines = append(lines, centerSty.MaxHeight(2).Render(v.content))
		} else {
			lines = append(lines, v.content)
		}
	}

	// compose the titles and inputs
	mainView := lipgloss.JoinVertical(lipgloss.Left, lines...)

	// generate submit button centered under the modal
	var sbtn = stylesheet.ViewSubmitButton(c.SubmitSelected(), setWidth, c.inputs.err, c.createErr)
	return lipgloss.NewStyle().AlignHorizontal(lipgloss.Left).Render(mainView) + "\n" + sbtn

}

// a material is a component view, collected from a Field.
type material struct {
	content  string
	toCenter bool // should this line be centered in the final composition
}

// collectViewValues gathers the material views, composes TitleValues, and measures the greatest line-width seen amongst the TitleValues.
//
// Returns the views (ordered as c.inputs.ordered) and the widest TitleView seen in the set.
// The width should be used to center lines tagged with toCenter.
func (c *createModel) collectViewValues() (views []material, setWidth int) {
	views = make([]material, 0, len(c.inputs.ordered))
	for i, key := range c.inputs.ordered {
		field := c.fields[key]
		kind, value, secondLine := c.fields[key].Provider.View(i == int(c.inputs.selected), c.width)

		switch kind {
		case TitleValue:
			// left-pad so all titles are right-aligned to a consistent column width
			padding := strings.Repeat(" ", c.longestTitleLength-len(field.Title))
			pip := stylesheet.Pip(c.inputs.selected, uint(i))
			var styledTitle string
			if field.Required {
				styledTitle = stylesheet.RequiredTitle(field.Title)
			} else {
				styledTitle = stylesheet.OptionalTitle(field.Title)
			}
			line := padding + pip + styledTitle + value
			views = append(views, material{content: line, toCenter: false})
			if w := lipgloss.Width(line); w > setWidth {
				setWidth = w
			}
		case Line:
			views = append(views, material{
				content:  stylesheet.Pip(c.inputs.selected, uint(i)) + value,
				toCenter: true,
			})
		}
		// attach second line, if provided; always centered
		if secondLine != "" {
			views = append(views, material{content: secondLine, toCenter: true})
		}
	}

	return views, setWidth
}

func (c *createModel) Done() bool {
	return c.mode == quitting
}

func (c *createModel) Reset() error {
	c.mode = inputting

	for _, key := range c.inputs.ordered {
		c.fields[key].Provider.Reset()
	}

	c.createErr = ""
	c.inputs.err = ""
	c.inputs.selected = 0
	c.focusInput(true)
	return nil
}

func (c *createModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (
	invalid string, onStart tea.Cmd, err error) {
	if err := c.fs.Parse(tokens); err != nil {
		return err.Error(), nil, nil
	}

	// call SetArg hooks
	for _, key := range c.inputs.ordered {
		c.fields[key].Provider.SetArgs(width, height)
	}

	if _, err := setValuesFromFlags(&c.fs, c.fields); err != nil {
		return "", nil, err
	}

	c.width = width

	// set the error immediately based on starting satisfaction states.
	// This is really just to set the error to the first missing required field's error.
	c.checkSatisfaction(false)
	return "", nil, nil
}

// attachLogInfo returns 3 SDParams that are useful to attach to most/every log: key, type, and caller identity.
func attachLogInfo(key string) []rfc5424.SDParam {
	return []rfc5424.SDParam{
		{Name: "field_key", Value: key},
		scaffold.IdentifyCaller(),
	}
}
