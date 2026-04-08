// Package pathtextinput provides a textinput bubble geared for navigating file structures.
// It is nothing more than the textinput bubble with a bit of dressing.
package pathtextinput

import (
	"os"
	"path"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	// NOTE(rlandau): if base.Value is empty, it will match no suggestions.
	// As we want it to match all suggestions when base.Value is empty (but attempt to complete none),
	// we need to wrap and hide base.
	textinput.Model
	pwd string
}

//var _ tea.Model = Model{}

type Options struct {
	PWD      string                 // Directory to traverse from. Defaults to os.Getwd().
	CustomTI func() textinput.Model // custom TI generator to use instead of textinput.New()
}

// New returns a ready-to-use Model.
// Notes:
//
// - Use AvailableSuggestions, not MatchedSuggestions.
// AvailableSuggestions is trimmed down to matching entries only in Update and MatchSuggestions will not work properly when Value is empty.
//
// - Sets ShowSuggestions, as there isn't much reason you'd use this without.
//
// Remember to focus it!
func New(opt Options) Model {
	m := Model{pwd: opt.PWD}
	if m.pwd == "" {
		dir, err := os.Getwd()
		if err != nil {
			// default to ".", though we cannot guarantee how that will operate
			dir = "."
		}
		m.pwd = dir
	}
	// ensure m.pwd ends with a slash
	if !strings.HasSuffix(m.pwd, "/") {
		m.pwd += "/"
	}
	if opt.CustomTI == nil {
		m.Model = textinput.New()
	} else {
		m.Model = opt.CustomTI()
	}

	// generate initial suggestions
	m.Model.SetSuggestions(deriveCompletions(m.pwd, m.Model.Value()))
	m.Model.ShowSuggestions = true
	m.Model, _ = m.Model.Update(tea.Key{Type: tea.KeyRunes})
	return m
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.Model, cmd = m.Model.Update(msg)
	// generate suggestions based on the current traversal
	if _, ok := msg.(tea.KeyMsg); ok {
		completions := deriveCompletions(m.pwd, m.Model.Value())
		m.Model.SetSuggestions(completions)
	}
	return m, cmd
}

// Returns the set of files available at the given path that prefix-match the last element.
// If an error occurs, it is swallowed and no completions are returned.
func deriveCompletions(root, input string) (completions []string) {
	var pth = input
	if !path.IsAbs(input) {
		pth = root + input
	}
	// if path ends with a slash, use the whole thing as the directory
	var dir, fn string
	if input == "" || strings.HasSuffix(input, "/") {
		dir = pth
	} else {
		dir, fn = path.Split(pth)
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, de := range des {
		if unmatchedRunes, found := strings.CutPrefix(de.Name(), fn); found {
			// to actually match and tab-complete, we need to ensure input is included
			completions = append(completions, input+unmatchedRunes)
		}
	}
	// underline the first suggestion
	if len(completions) > 0 {
		completions[0] = lipgloss.NewStyle().Underline(true).Render(completions[0])
	}
	return completions
}

// PWD returns the directory that this pti is operating (and basing all paths) out of.
func (m Model) PWD() string {
	return m.pwd
}
