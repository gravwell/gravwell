package scaffoldcreate

import (
	"fmt"
	"path"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/pathtextinput"
)

type ViewKind uint

const (
	// default view kind. Returns bifurcated data: title text and value.
	TitleValue ViewKind = iota
	Line                // data is displayed as a single block, centered relative to other fields
	// this view wants to take over the entire view for this cycle.
	// View processing stops on the first takeover.
	Takeover
)

// A FieldProvider defines the contract fields must provide to be usable with create.
type FieldProvider interface {
	// initialize the instance, fetching required data.
	// This is only called once, at tree-construction time.
	Initialize()
	// Reset the instance back to its initial, ready-for-use state.
	// Called after the action's invocation completes.
	Reset()
	Update(selected bool, msg tea.Msg) tea.Cmd
	// View for the Provider.
	// Kind tells scaffoldcreate how to display this view and if it should continue to process the Views of other fields.
	// Title will only be used if ViewKind == TitleValue.
	//
	// SecondLine contains content that will be displayed below the title+value or single line.
	// It is not shown if Kind == Takeover or if secondLine == "".
	View(selected bool, width int) (_ ViewKind, title, value, secondLine string)
	// Is this field done and ready to be submitted or is something about it invalid?
	Satisfied() (invalid string)

	// Try to set val into this provider.
	Set(val string) (invalid string)
}

var _ FieldProvider = &TextProvider{}
var _ FieldProvider = &PathProvider{}
var _ FieldProvider = &MSLProvider{}

type TextProvider struct {
	Title string
	ti    textinput.Model

	// Function to use to create the base textinput instead of stylesheet.NewTI().
	// Useful for setting validator funcs.
	CustomInit func() textinput.Model
	// Function to call each Reset instead of textinput.Reset().
	CustomReset func(textinput.Model) textinput.Model
}

func (p *TextProvider) Initialize() {
	if p.CustomInit != nil {
		p.ti = p.CustomInit()
	} else {
		p.ti = stylesheet.NewTI("", false)
	}
}

func (p *TextProvider) Reset() {
	if p.CustomReset != nil {
		p.ti = p.CustomReset(p.ti)
		return
	}
	p.ti.Reset()
}

func (p *TextProvider) Update(_ bool, msg tea.Msg) (cmd tea.Cmd) {
	p.ti, cmd = p.ti.Update(msg)
	return
}

func (p *TextProvider) View(_ bool, _ int) (_ ViewKind, title, value, _ string) {
	return TitleValue, p.Title, p.ti.View(), ""
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

type PathProvider struct {
	Title string
	pti   pathtextinput.Model

	Options pathtextinput.Options
}

func (p *PathProvider) Initialize() {
	p.pti = pathtextinput.New(p.Options)
}

func (p *PathProvider) Reset() {
	p.pti.Reset()
}

func (p *PathProvider) Update(_ bool, msg tea.Msg) (cmd tea.Cmd) {
	p.pti, cmd = p.pti.Update(msg)
	return
}

// the number of lines available for use when showing path suggestions.
// If fewer suggestions are available than populate these lines, View will pad vertically
const pathSuggestionLineCount int = 2

func (p *PathProvider) View(_ bool, width int) (_ ViewKind, title, value, secondLine string) {
	sgts := TrimSuggestsToFile(p.pti.MatchedSuggestions(), p.pti.Value())
	// truncate suggestions to a single line, within the max size of field+input
	secondLine = lipgloss.NewStyle().
		Width(width).
		MaxHeight(pathSuggestionLineCount).
		Height(pathSuggestionLineCount).
		Render(strings.Join(sgts, " "))
	return TitleValue, p.Title, p.pti.View(), secondLine
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

type MSLProvider struct {
	Title string // the field title selection will be associated to

	singular, plural string

	msl multiselectlist.Model

	// number of items currently selected
	// cached after we leave takeover so we don't have to recalculate each view
	numSelected int

	// is the MSLProvider currently facilitating a user selecting items?
	takeover bool

	Items   []list.DefaultItem
	Options multiselectlist.Options
	// Requires at least X items to be selected before this Provider is considered satisfied.
	RequireAtLeast uint
	// Requires no more than X items to be selected before this Provider is considered satisfied.
	// 0 means disabled.
	RequireAtMost uint
}

func NewMSLProvider(fieldTitle string, items []list.DefaultItem, opts multiselectlist.Options) *MSLProvider {
	return &MSLProvider{Title: fieldTitle, Items: items, Options: opts}
}

func (p *MSLProvider) Initialize() {
	p.msl = multiselectlist.New(p.Items, 80, 60, p.Options)
}

func (p *MSLProvider) Reset() {
	p.msl = multiselectlist.New(p.Items, p.msl.Width(), p.msl.Height(), p.Options)
	p.msl.Undone()
}

func (p *MSLProvider) Update(_ bool, msg tea.Msg) (cmd tea.Cmd) {
	p.msl, cmd = p.msl.Update(msg)
	return
}

func (p *MSLProvider) View(selected bool, _ int) (_ ViewKind, title, value, secondLine string) {
	// if the msl is currently in selection mode, we need to return a takeover
	if p.takeover {
		// sanity check that we are currently selected;
		// if we are in takeover mode and not selected, something is probably wrong.
		if !selected {
			clilog.Writer.Warnf("MSL provider is in takeover mode, but is not selected!")
		}
		return Takeover, "", p.msl.View(), ""
	}
	// otherwise, we return "<title>: ->select<-" and the number of selected items

	value = "select"
	if selected {
		value = stylesheet.RightSigil + value + stylesheet.LeftSigil
	}

	secondLine = fmt.Sprintf("%d %s currently selected", p.numSelected, phrases.NounNumerosity(p.numSelected, p.singular, p.plural))

	return TitleValue, p.Title, value, secondLine
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

// Set expects values to be passed as comma-separated values
func (p *MSLProvider) Set(val string) (invalid string) {
	val = strings.TrimSpace(val)
	if val == "" {
		return ""
	}
	vals := strings.Split(val, ",")
	_, firstNotFound := p.msl.SelectItems(vals)
	if firstNotFound != "" {
		return firstNotFound + " is not an available " + p.singular
	}
	return ""
}
