/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldcreate

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/sigils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/pathtextinput"
)

type ViewKind uint

const (
	// default view kind. Returns bifurcated data: title text and value.
	TitleValue ViewKind = iota
	Line                // data is displayed as a single block, centered relative to other fields
	// This provider is in takeover mode and will provide the view for the entire pane.
	// View processing stops on the first takeover.
	Takeover
)

// A FieldProvider defines the contract fields must provide to be usable with create.
type FieldProvider interface {
	// initialize the instance, fetching required data.
	// This is only called once, at tree-construction time.
	Initialize(required bool)
	// Reset the instance back to its initial, ready-for-use state.
	// Called after the action's invocation completes.
	Reset()
	// Hijack SetArgs to alter/set data before the user can interact with/see the provider.
	// This is called BEFORE flags are parsed into their fields.
	// Does not pass in flagset or tokens as we don't want fields interacting with raw data: complexity management.
	SetArgs(width, height int)
	// Update for the Provider.
	// Takeover tells scaffoldcreate that this field would like to assert control over the action.
	// This bool takes effect immediately and will be reflected in the following view.
	// In takeover mode, all updates will be passed directly to this Provider.
	// scaffoldcreate will reassert control as soon as takeover returns false or the user provides a soft kill key (soft kill keys NYI).
	Update(selected bool, msg tea.Msg) (_ tea.Cmd, takeover bool)
	// View for the Provider.
	// Kind tells scaffoldcreate how to display this view and if it should continue to process the Views of other fields.
	//
	// SecondLine contains content that will be displayed below the title+value or single line.
	// It is not shown if Kind == Takeover or if secondLine == "".
	View(selected bool, width int) (_ ViewKind, value, secondLine string)
	// Is this field done and ready to be submitted or is something about it invalid?
	Satisfied() (invalid string)

	// Try to set val into this provider.
	Set(val string) (invalid string)

	// Get the current value of the field as a string.
	Get() string
	// ToggleFocus focuses or blurs this provider.
	ToggleFocus(focus bool)
}

var _ FieldProvider = &TextProvider{}
var _ FieldProvider = &PathProvider{}
var _ FieldProvider = &MSLProvider{}
var _ FieldProvider = &BoolProvider{}

type TextProvider struct {
	// value to default the TI to
	InitialValue string
	ti           textinput.Model

	// Function to use to create the base textinput instead of stylesheet.NewTI().
	// Useful for setting validator func that do not rely on external data.
	CustomInit func() textinput.Model
	// Function to call each Reset instead of textinput.Reset().
	CustomReset func(textinput.Model) textinput.Model
	// Used to hook SetArgs for custom alterations at each action invocation.
	// Useful for setting suggestions/validation based on current data.
	CustomSetArgs func(textinput.Model) textinput.Model
}

func (p *TextProvider) Initialize(required bool) {
	if p.CustomInit != nil {
		p.ti = p.CustomInit()
		// set default value
		p.ti.SetValue(p.InitialValue)
	} else {
		p.ti = stylesheet.NewTI(p.InitialValue, !required)
		p.ti.Width = 30
	}
}

func (p *TextProvider) Reset() {
	if p.CustomReset != nil {
		p.ti = p.CustomReset(p.ti)
		return
	}
	p.ti.Reset()
}

func (p *TextProvider) SetArgs(_, _ int) {
	if p.CustomSetArgs != nil {
		p.ti = p.CustomSetArgs(p.ti)
	}
}

func (p *TextProvider) Update(_ bool, msg tea.Msg) (cmd tea.Cmd, takeover bool) {
	p.ti, cmd = p.ti.Update(msg)
	return cmd, false
}

func (p *TextProvider) View(_ bool, _ int) (_ ViewKind, value, _ string) {
	return TitleValue, p.ti.View(), ""
}

func (p *TextProvider) Satisfied() (invalid string) {
	if p.ti.Err != nil {
		return p.ti.Err.Error()
	}
	return ""
}

func (p *TextProvider) Set(val string) (invalid string) {
	p.ti.SetValue(val)
	if p.ti.Err != nil {
		return p.ti.Err.Error()
	}
	return ""
}

func (p *TextProvider) Get() string {
	return p.ti.Value()
}

func (p *TextProvider) ToggleFocus(focus bool) {
	if focus {
		p.ti.Focus()
		return
	}
	p.ti.Blur()
}

type PathProvider struct {
	pti pathtextinput.Model

	InitialValue string
	Options      pathtextinput.Options
}

func (p *PathProvider) Initialize(required bool) {
	if p.Options.CustomTI == nil {
		p.Options.CustomTI = func() textinput.Model {
			ti := stylesheet.NewTI(p.InitialValue, !required)
			ti.Width = 30 // override TI width
			return ti
		}
	}
	p.pti = pathtextinput.New(p.Options)
	p.pti.SetValue(p.InitialValue)
}

func (p *PathProvider) Reset() {
	p.pti.Reset()
}

func (p *PathProvider) SetArgs(_, _ int) {}

func (p *PathProvider) Update(_ bool, msg tea.Msg) (cmd tea.Cmd, takeover bool) {
	p.pti, cmd = p.pti.Update(msg)
	return cmd, false
}

// the number of lines available for use when showing path suggestions.
// If fewer suggestions are available than populate these lines, View will pad vertically
const pathSuggestionLineCount int = 2

func (p *PathProvider) View(_ bool, _ int) (_ ViewKind, value, secondLine string) {
	sgts := TrimSuggestsToFile(p.pti.AvailableSuggestions(), p.pti.Value())
	// truncate suggestions to a single line, within the max size of field+input
	secondLine = lipgloss.NewStyle().
		//Width(width).
		//MaxHeight(pathSuggestionLineCount).
		Height(pathSuggestionLineCount).
		Render(strings.Join(sgts, " "))
	return TitleValue, p.pti.View(), secondLine
}

// TrimSuggestsToFile is a helper function for View that returns only the file chunk of each suggestion, with matching runes colourized.
// The dir portion, if it exists, is thrown away.
// These suggestions should not be fed into a TI; they are intended for display to a user.
//
// If a suggested filename does not contain matching characters in input, it will be dropped.
// This is mostly because this function expects the suggestions to already be trimmed down to matches only;
// if there is a mismatch, something has likely gone wrong.
func TrimSuggestsToFile(availSgts []string, input string) (filenames []string) {
	for _, sgt := range availSgts {
		sgtDir, sgtFn := path.Split(sgt)
		partialFN := strings.TrimPrefix(input, sgtDir)
		// strip off matching file characters
		unmatchedFNRunes, found := strings.CutPrefix(sgtFn, partialFN)
		if !found {
			clilog.Writer.Warnf("dropping suggestion '%v'; the input filename '%v' does not prefix-match filename '%v'",
				sgt, partialFN, sgtFn)
			continue
		}
		// colourize and reattach matching file characters
		filenames = append(filenames, stylesheet.Cur.TertiaryText.Render(partialFN)+unmatchedFNRunes)
	}

	return filenames
}

func (p *PathProvider) Satisfied() (invalid string) {
	if p.pti.Err != nil {
		return p.pti.Err.Error()
	}
	return ""
}

func (p *PathProvider) Set(val string) (invalid string) {
	p.pti.SetValue(val)
	if p.pti.Err != nil {
		return p.pti.Err.Error()
	}
	return ""
}

func (p *PathProvider) Get() string {
	return p.pti.Value()
}

func (p *PathProvider) ToggleFocus(focus bool) {
	if focus {
		p.pti.Focus()
		return
	}
	p.pti.Blur()
}

// MSLProvider provides a multiselect list field.
// Use NewMSLProvider() constructor.
//
// ! MSLProviders ignore default value.
type MSLProvider struct {
	singular, plural string

	msl multiselectlist.Model[string]

	// number of items currently selected
	// cached after we leave takeover so we don't have to recalculate each view
	numSelected int

	// is the MSLProvider currently facilitating a user selecting items?
	takeover bool

	BaseItems []multiselectlist.SelectableItem[string]
	Options   MSLOptions
	// Requires at least X items to be selected before this Provider is considered satisfied.
	RequireAtLeast uint
	// Requires no more than X items to be selected before this Provider is considered satisfied.
	// 0 means disabled.
	RequireAtMost uint
}

type MSLOptions struct {
	ListOptions multiselectlist.Options

	// Called during SetArgs, this function allows you to alter the list of items displayed by this MSL.
	// Called after height and width are set but before flags are parsed into the field.
	//
	// Returning nil will cause no items to be displayed.
	SetArgsInsertItems func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string])
}

// NewMSLProvider constructs a new multiselect list with the given items and options.
//
// ! MSLProviders ignore default value.
func NewMSLProvider(items []multiselectlist.SelectableItem[string], opts MSLOptions) *MSLProvider {
	return &MSLProvider{BaseItems: items, Options: opts}
}

func (p *MSLProvider) Initialize(_ bool) {
	p.msl = multiselectlist.New(p.BaseItems, 80, 60, p.Options.ListOptions)
	hotkeys.ApplyToList(&p.msl.KeyMap)
	p.numSelected = len(p.msl.GetSelectedItems())
}

func (p *MSLProvider) Reset() {
	p.msl = multiselectlist.New(p.BaseItems, p.msl.Width(), p.msl.Height(), p.Options.ListOptions)
	hotkeys.ApplyToList(&p.msl.KeyMap)
	p.numSelected = len(p.msl.GetSelectedItems())
	p.msl.Undone()
	p.takeover = false
}

func (p *MSLProvider) SetArgs(width, height int) {
	p.msl.SetWidth(width)
	p.msl.SetHeight(height)
	if p.Options.SetArgsInsertItems != nil {
		p.BaseItems = p.Options.SetArgsInsertItems(p.BaseItems)
		_ = p.msl.SetItems(p.BaseItems)
	}
}

func (p *MSLProvider) Update(selected bool, msg tea.Msg) (cmd tea.Cmd, tko bool) {
	// if the message is a window size message, it should always be passed to the list
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		p.msl, cmd = p.msl.Update(wsm)
		return cmd, p.takeover
	}

	defer func() {
		p.numSelected = len(p.msl.GetSelectedItems())
	}()
	// if we are already in takeover mode, just hand off control
	if p.takeover {
		p.msl, cmd = p.msl.Update(msg)
		// if we are done, exit takeover mode and undone (so a user can access this field again)
		p.takeover = !p.msl.Done()
		p.msl.Undone()
		return cmd, p.takeover
	}

	// check for takeover mode invocation
	if selected && hotkeys.Match(msg, hotkeys.Select) {
		p.takeover = true
		return nil, true
	}

	return nil, false
}

func (p *MSLProvider) View(selected bool, _ int) (_ ViewKind, value, secondLine string) {
	// if the msl is currently in selection mode, we need to return a takeover
	if p.takeover {
		// sanity check that we are currently selected;
		// if we are in takeover mode and not selected, something is probably wrong.
		if !selected {
			clilog.Writer.Warnf("MSL provider is in takeover mode, but is not selected!")
		}
		return Takeover, p.msl.View(), ""
	}
	// otherwise, we return "<title>: ->select<-" and the number of selected items

	value = "select"
	if selected {
		value = sigils.Right + value + sigils.Left
	}

	secondLine = fmt.Sprintf("%d %s currently selected", p.numSelected, phrases.NounNumerosity(p.numSelected, p.singular, p.plural))

	return TitleValue, value, secondLine
}

func (p *MSLProvider) Satisfied() (invalid string) {
	if p.numSelected < int(p.RequireAtLeast) {
		return fmt.Sprintf("you must select at least %d %s", p.RequireAtLeast, phrases.NounNumerosity(p.numSelected, p.singular, p.plural))
	}
	if p.RequireAtMost != 0 && (p.numSelected > int(p.RequireAtMost)) {
		return fmt.Sprintf("you must select at most %d %s", p.RequireAtMost, phrases.NounNumerosity(p.numSelected, p.singular, p.plural))
	}
	return ""
}

// Set selects items with matching titles. Expects values to be passed as comma-separated values.
//
// Ex: val1,val2,val3
func (p *MSLProvider) Set(val string) (invalid string) {
	val = strings.TrimSpace(val)
	if val == "" {
		return ""
	}
	vals := strings.Split(val, ",")
	_, notFound := p.msl.SelectItems(vals)
	if len(notFound) > 0 {
		return fmt.Sprintf("IDs %v not found", notFound)
	}
	return ""
}

// Get returns the set of selected items as a comma-separated list.
func (p *MSLProvider) Get() string {
	dis := p.msl.GetSelectedItems()
	var ss = make([]string, len(dis))
	for i, di := range dis {
		ss[i] = di.ID()
	}
	return strings.Join(ss, ",")
}

func (p *MSLProvider) ToggleFocus(_ bool) {
	// MSL doesn't actually care if it is in focus
}

type BoolProvider struct {
	Initial bool // starter value to be .Reset() to
	state   bool
}

// Initialize sets value to BooleanProvider.Initial.
func (p *BoolProvider) Initialize(_ bool) { p.Reset() }

// Reset returns value to .Initial
func (p *BoolProvider) Reset() { p.state = p.Initial }

// SetArgs has no effect.
func (p *BoolProvider) SetArgs(_, _ int) {}

func (p *BoolProvider) Update(selected bool, msg tea.Msg) (_ tea.Cmd, takeover bool) {
	if selected && hotkeys.Match(msg, hotkeys.Select) {
		p.state = !p.state
	}
	return nil, false
}

func (p *BoolProvider) View(selected bool, width int) (_ ViewKind, value, secondLine string) {
	return TitleValue, stylesheet.Checkbox(p.state), ""
}

// Satisfied is never false for Booleans; what would be the point of that?
func (p *BoolProvider) Satisfied() (invalid string) {
	return ""
}

// Set uses strconv.ParseBool.
func (p *BoolProvider) Set(val string) (invalid string) {
	if val = strings.TrimSpace(val); val == "" {
		return ""
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return err.Error()
	}
	p.state = b
	return
}

// Get uses strconv.FormatBool.
func (p *BoolProvider) Get() string {
	return strconv.FormatBool(p.state)
}

func (p *BoolProvider) ToggleFocus(focus bool) {}
