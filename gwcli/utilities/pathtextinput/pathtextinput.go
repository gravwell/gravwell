// Package pathtextinput provides a textinput bubble geared for navigating file structures.
// It is nothing more than the textinput bubble with a bit of dressing.
package pathtextinput

import (
	"os"
	"path"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type Model struct {
	// NOTE(rlandau): if base.Value is empty, it will match no suggestions.
	// As we want it to match all suggestions when base.Value is empty (but attempt to complete none),
	// we need to wrap and hide base.
	textinput.Model
	pwd string

	defaultPWD string
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
	m := Model{
		pwd: opt.PWD,
	}
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
	m.defaultPWD = m.pwd

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
	var fullPath string
	switch expanded := ExpandTilde(input); {
	case expanded != input:
		fullPath = expanded
	case path.IsAbs(input):
		fullPath = input
	default:
		fullPath = root + input
	}

	// echo "~/" instead of "~" so suggestions read "~/dir1", not "~dir1"
	prefix := input
	if input == "~" {
		prefix = "~/"
	}

	// if path ends with a slash, use the whole thing as the directory
	// (bare "~" counts too - there's nothing left to split off)
	var dir, fn string
	if input == "" || input == "~" || strings.HasSuffix(input, "/") {
		dir = fullPath
	} else {
		dir, fn = path.Split(fullPath)
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, de := range des {
		if unmatchedRunes, found := strings.CutPrefix(de.Name(), fn); found {
			// to actually match and tab-complete, we need to ensure prefix is included
			completions = append(completions, prefix+unmatchedRunes)
		}
	}
	return completions
}

// ExpandTilde expands a bare "~" or "~/..." to the user's home directory, like bash does.
// Anything else starting with "~" (e.g. "~user") is left alone, as is everything if the
// home directory can't be determined.
func ExpandTilde(input string) string {
	if input != "~" && !strings.HasPrefix(input, "~/") {
		return input
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return input
	}

	return path.Join(home, strings.TrimPrefix(input, "~"))
}

// PWD returns the directory that this pti is operating (and basing all paths) out of.
func (m Model) PWD() string {
	return m.pwd
}

// Reset resets the TI, returns it to the PWD defined by options, and blurs.
func (m *Model) Reset() {
	m.Model.Reset()
	m.pwd = m.defaultPWD
	m.Model.SetSuggestions(deriveCompletions(m.pwd, m.Model.Value()))
	m.Model.Blur()
}
