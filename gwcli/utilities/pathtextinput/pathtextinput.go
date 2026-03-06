// package pathti provides a textinput bubble geared for navigating file structures.
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
	base textinput.Model
	pwd  string
}

//var _ tea.Model = Model{}

type Options struct {
	Root     string                 // Directory to traverse from. Defaults to os.Getwd().
	CustomTI func() textinput.Model // custom TI generator to use instead of textinput.New()
}

// New returns a ready-to-use Model.
// Sets ShowSuggestions, as there isn't much reason you'd use this without.
//
// Remember to focus it!
func New(opt Options) Model {
	m := Model{pwd: opt.Root}
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
		m.base = textinput.New()
	} else {
		m.base = opt.CustomTI()
	}

	// generate initial suggestions
	m.base.SetSuggestions(deriveCompletions(m.pwd, m.base.Value()))
	m.base.ShowSuggestions = true
	m.base, _ = m.base.Update(tea.Key{Type: tea.KeyRunes})
	return m
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.base, cmd = m.base.Update(msg)
	// generate suggestions based on the current traversal
	completions := deriveCompletions(m.pwd, m.base.Value())
	if _, ok := msg.(tea.KeyMsg); ok {
		m.base.SetSuggestions(completions)
	}
	return m, cmd
}

func (m Model) View() string {
	return m.base.View()
}

func (m *Model) Focus() {
	m.base.Focus()
}

func (m *Model) Blur() {
	m.base.Blur()
}

func (m *Model) SetValue(s string) {
	m.base.SetValue(s)
}

func (m Model) Suggestions() []string {
	return m.base.AvailableSuggestions()
}

// Returns the set of files available at the given path that prefix-match the last element.
func deriveCompletions(root, relPath string) (completions []string) {
	fullpath := path.Join(root, relPath)
	// if path ends with a slash, use the whole thing as the directory
	var dir, last string
	if relPath == "" || strings.HasSuffix(relPath, "/") {
		dir = fullpath
	} else {
		dir, last = path.Split(fullpath)
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, de := range des {
		if strings.HasPrefix(de.Name(), last) {
			completions = append(completions, de.Name())
		}
	}
	return completions
}
