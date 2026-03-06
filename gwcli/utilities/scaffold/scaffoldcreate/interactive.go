package scaffoldcreate

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/pathtextinput"
)

// An interact is any item capable of being displayed and interacted with by a user in the scaffoldcreate interactive modal.
// Unlike a typical tea.Model, assumes
type interact interface {
	Key() string
	Update(tea.Msg) tea.Cmd // assumes a pointer receiver, unlike a normal update
	HasNextLine() bool      // Returns if this kind of interact has data it wishes to display on a following line.
	NextLine() string       // Returns the data intended for the next line. If !HasNextLine(), should always return "".
	Error() error
	SetValue(string) error
	Value() string // Returns the right half (field half) of the interact to be paired with the title.

	Focus()
	Blur()
	Reset()
}

//#region wrappers around types to fit the interact interface

type ptiInteract struct {
	model pathtextinput.Model
	key   string
}

func (i *ptiInteract) Key() string {
	return i.key
}

func (i *ptiInteract) Update(msg tea.Msg) (cmd tea.Cmd) {
	i.model, cmd = i.model.Update(msg)
	return cmd
}
func (i *ptiInteract) HasNextLine() bool {
	return true
}

// NextLine returns
func (i *ptiInteract) NextLine() string {
	return
}
func (i *ptiInteract) Error() error
func (i *ptiInteract) SetValue(string)
func (i *ptiInteract) Value() string
