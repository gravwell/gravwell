// package pathti provides a textinput bubble geared for navigating file structures.
// It is nothing more than the textinput bubble with a bit of dressing.
package pathtextinput

import (
	"os"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type Model struct {
	textinput.Model
	pwd string
}

var _ tea.Model = Model{}

type Options struct {
	PWD      string                 // present working directory. Defaults to os.Getwd().
	CustomTI func() textinput.Model // custom TI generator to use instead of textinput.New()
}

// New returns a ready-to-use Model.
func New(opt Options) Model {
	m := Model{}
	if opt.PWD == "" {
		dir, err := os.Getwd()
		if err != nil {
			// default to ".", though we cannot guarantee how that will operate
			dir = "."
		}
		m.pwd = dir
	}
	if opt.CustomTI == nil {
		m.Model = textinput.New()
	} else {
		m.Model = opt.CustomTI()
	}

	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.Model, cmd = m.Model.Update(msg)
	return m, cmd
}

func (m Model) View() string {

}

// Suggestions returns the current set of completions.
// Suggestions are suffixes for Value.
func (m Model) Suggestions() []string {

}
