package ingest

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/colorizer"
)

// currently selected item in the mod view for pips and focus
type modItem = uint

const (
	lowBound modItem = iota
	src
	tag
	ignoreTS
	localTime
	highBound
)

// mod struct represents the state of the modifier/excess details pane
type mod struct {
	// meta
	focused  bool    // is the modifier pane in focus?
	selected modItem // currently selected item in the mod pane
	width    uint

	tagTI     textinput.Model // tag to ingest file under
	srcTI     textinput.Model // user-provided IP address source
	ignoreTS  bool
	localTime bool
}

func NewMod() mod {
	m := mod{
		focused:  false,
		selected: src,

		tagTI: stylesheet.NewTI("", true),
		srcTI: stylesheet.NewTI("default", true),
	}
	m.srcTI.Placeholder = "127.0.0.1"
	m.srcTI.Focus()
	m.tagTI.Blur()
	return m
}

// Does not handle enter or tab; caller is expected to catch and process these before handing off control.
func (m mod) update(msg tea.Msg) (mod, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyLeft:
			m.selected -= 1
			if m.selected <= lowBound {
				m.selected = highBound - 1
			}
			m.focusSelected()
		case tea.KeyRight:
			m.selected += 1
			if m.selected >= highBound {
				m.selected = lowBound + 1
			}
			m.focusSelected()
		case tea.KeySpace:
			// toggle the selected boolean
			switch m.selected {
			case ignoreTS:
				m.ignoreTS = !m.ignoreTS
			case localTime:
				m.localTime = !m.localTime
			}
		}
	}
	var cmds = []tea.Cmd{nil, nil}
	m.srcTI, cmds[0] = m.srcTI.Update(msg)
	m.tagTI, cmds[1] = m.tagTI.Update(msg)

	return m, tea.Batch(cmds...)
}

func (m mod) view() string {
	v := fmt.Sprintf(
		"%vsource: %s\t"+
			"%vtag: %s\t"+
			"%vIgnore Timestamps? %v\t"+
			"%vUse Server Local Time? %v\t",
		colorizer.Pip(m.selected, src), m.srcTI.View(),
		colorizer.Pip(m.selected, tag), m.tagTI.View(),
		colorizer.Pip(m.selected, ignoreTS), colorizer.Checkbox(m.ignoreTS),
		colorizer.Pip(m.selected, localTime), colorizer.Checkbox(m.localTime),
	)

	if m.focused {
		return stylesheet.Sheet.Composable.FocusedBorder.Width(int(m.width) - (stylesheet.Sheet.Composable.FocusedBorder.GetHorizontalMargins() + 2)).Render(v)
	} else {
		return stylesheet.Sheet.Composable.UnfocusedBorder.Width(int(m.width) - (stylesheet.Sheet.Composable.UnfocusedBorder.GetHorizontalMargins() + 2)).Render(v)
	}
}

// Returns a mod view that has been returned to its initial form and is ready for re-use.
func (m mod) reset() mod {
	m.focused = false
	m.tagTI.Reset()
	m.srcTI.Reset()
	m.ignoreTS = false
	m.localTime = false

	m.srcTI.Focus()
	m.tagTI.Blur()

	return m
}

// update the focus/blur settings to field corresponding to the current enumeration of m.selected.
func (m *mod) focusSelected() {
	switch m.selected {
	case src:
		m.srcTI.Focus()
		m.tagTI.Blur()
	case tag:
		m.srcTI.Blur()
		m.tagTI.Focus()
	default:
		m.srcTI.Blur()
		m.tagTI.Blur()
	}
}
