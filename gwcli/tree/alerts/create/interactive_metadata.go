package alertscreate

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
)

// fieldnum identifies each field numerically so we can figure out which one is currently selected
type fieldNum uint

const (
	numName fieldNum = iota
	numDescription
	numTag
	numEnable
	numMaxEvents
	numRetain
	numBackToDispatchers // return to dispatcher selection stage
	numBackToConsumers   // return to consumer selection stage
	numSubmit
)

type metadata struct {
	inputErr  string // a validation error from one of the below inputs
	submitErr string // error returned by the last submit attempt
	selected  fieldNum

	// required

	name textinput.Model

	// optional

	description textarea.Model
	tag         textinput.Model
	enable      bool
	maxEvents   textinput.Model // convert to int on submit
	retain      textinput.Model // read as time.Duration, converted to seconds on submit
}

func NewMetadata() *metadata {
	m := &metadata{
		name:        stylesheet.NewTI("", false),
		description: textarea.New(),
		tag:         stylesheet.NewTI("", true),
		maxEvents:   stylesheet.NewTI("", true),
		retain:      stylesheet.NewTI("", true),
	}

	m.tag.Placeholder = "_alerts"
	m.maxEvents.Validate = func(s string) error {
		if s == "" {
			return nil
		}

		for _, r := range s {
			if !unicode.IsNumber(r) {
				return errors.New("Max Events must be numeric")
			}
		}
		return nil
	}
	m.retain.Placeholder = "1h00m00s"
	m.retain.Validate = func(s string) error {
		if s == "" {
			return nil
		}
		if t, err := time.ParseDuration(s); err != nil {
			return err
		} else if t.Abs() != t {
			return errors.New("retain time must be positive")
		}
		return nil
	}
	return m
}

const titleLength = len("description") + 1 // compose titles based on the longest title +1 (additional left pad)

// Init sets initial values into metadata.
// It is safe to use metadata without Init, but good practice to call it just in case.
func (m *metadata) Init(name, description, tag string, enable bool, maxEvents int, retainS int32) {
	m.name.SetValue(name)
	m.description.SetValue(description)
	m.tag.SetValue(tag)
	m.enable = enable
	m.maxEvents.SetValue(strconv.FormatInt(int64(maxEvents), 10))
	if retainS != 0 {
		m.retain.SetValue(strconv.FormatInt(int64(retainS), 10))
	}
}

func (m *metadata) Update(msg tea.Msg) (_ tea.Cmd, backToDispatchers, backToConsumers, trySubmit bool) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		m.submitErr = "" // clear error from last create attempt
		switch keyMsg.Type {
		case tea.KeyShiftTab, tea.KeyShiftUp:
			m.focusPrevious()
			return textinput.Blink, false, false, false
		case tea.KeyTab, tea.KeyShiftDown:
			m.focusNext()
			return textinput.Blink, false, false, false
		case tea.KeySpace:
			if m.selected == numEnable {
				m.enable = !m.enable
			}
		case tea.KeyEnter:
			// handle buttons and booleans
			switch m.selected {
			case numBackToDispatchers:
				return nil, true, false, false
			case numBackToConsumers:
				return nil, false, true, false
			case numSubmit:
				// check that we are in a valid state
				if m.inputErr == "" {
					return nil, false, false, true
				}
			}
		}
	}

	// pass the message into the appropriate text input

	m.inputErr = ""
	var cmd tea.Cmd
	switch m.selected {
	case numName:
		m.name, cmd = m.name.Update(msg)
		if m.name.Err != nil {
			m.inputErr = m.name.Err.Error()
		}
	case numDescription:
		m.description, cmd = m.description.Update(msg)
		if m.description.Err != nil {
			m.inputErr = m.description.Err.Error()
		}
	case numTag:
		m.tag, cmd = m.tag.Update(msg)
		if m.tag.Err != nil {
			m.inputErr = m.tag.Err.Error()
		}
	case numMaxEvents:
		m.maxEvents, cmd = m.maxEvents.Update(msg)
		if m.maxEvents.Err != nil {
			m.inputErr = m.maxEvents.Err.Error()
		}
	case numRetain:
		m.retain, cmd = m.retain.Update(msg)
		if m.retain.Err != nil {
			m.inputErr = m.retain.Err.Error()
		}
	}
	return cmd, false, false, false
}

// Blurs the current input, selects and focuses the next one c.inputs.ordered.
func (m *metadata) focusNext() {
	m.toggleFocus(false)
	if m.selected == numSubmit { // jump to start
		m.selected = 0
	} else {
		m.selected += 1
	}
	m.toggleFocus(true)
}

// Blurs the current input, selects and focuses the previous one in c.inputs.ordered.
func (m *metadata) focusPrevious() {
	m.toggleFocus(false)

	if m.selected == 0 { // wrap to submit button
		m.selected = numSubmit
	} else {
		m.selected -= 1
	}
	m.toggleFocus(true)
}

// toggleFocus toggles the focus on the currently selected input (doing nothing if a non-TI/TA is selected).
// If !focus, blurs the input.
func (m *metadata) toggleFocus(focus bool) {
	if m.submitSelected() {
		return
	}

	switch m.selected {
	case numName:
		if focus {
			m.name.Focus()
		} else {
			m.name.Blur()
		}
	case numDescription:
		if focus {
			m.description.Focus()
		} else {
			m.description.Blur()
		}
	case numTag:
		if focus {
			m.tag.Focus()
		} else {
			m.tag.Blur()
		}
	case numMaxEvents:
		if focus {
			m.maxEvents.Focus()
		} else {
			m.maxEvents.Blur()
		}
	case numRetain:
		if focus {
			m.retain.Focus()
		} else {
			m.retain.Blur()
		}
	case numEnable, numBackToDispatchers, numBackToConsumers, numSubmit:
	default:
		s := "focus"
		if !focus {
			s = "blur"
		}
		clilog.Writer.Errorf("failed to %s input: unknown field number %v selected", s, m.selected)
	}
}

func (m *metadata) submitSelected() bool {
	return m.selected == numSubmit
}

func (m *metadata) View() string {
	var sb strings.Builder

	m.viewline(&sb, true, "Name", numName, m.name.View())
	m.viewline(&sb, false, "Description", numDescription, m.description.View())
	m.viewline(&sb, false, "Tag", numTag, m.tag.View())
	m.viewline(&sb, false, "Enable", numEnable, stylesheet.Checkbox(m.enable))
	m.viewline(&sb, false, "Max Events", numMaxEvents, m.maxEvents.View())
	m.viewline(&sb, false, "Retain", numRetain, m.retain.View())

	// "back to" buttons
	sb.WriteString(
		lipgloss.JoinHorizontal(lipgloss.Center,
			stylesheet.Pip(uint(m.selected), uint(numBackToDispatchers)),
			stylesheet.Button("back to dispatcher selection")))
	sb.WriteRune('\n')
	sb.WriteString(
		lipgloss.JoinHorizontal(lipgloss.Center,
			stylesheet.Pip(uint(m.selected), uint(numBackToConsumers)),
			stylesheet.Button("back to consumer selection")))
	sb.WriteRune('\n')

	sb.WriteString(stylesheet.ViewSubmitButton(m.selected == numSubmit, titleLength*2, m.inputErr, m.submitErr))

	// attach faux-help
	// TODO at some point, we should replace this with actual help and real key binds.
	sb.WriteString("\n\n" +
		stylesheet.Cur.DisabledText.Render(
			"shift+"+stylesheet.UpDownSigils+": scroll • space: toggle • enter: interact"+
				"\ntab: cycle • esc: quit"))
	return sb.String()
}

// helper function for View to compose and align a given line as <padding><pip><title>: <view>\n
func (m *metadata) viewline(sb *strings.Builder, required bool, title string, num fieldNum, view string) {
	left := strings.Repeat(" ", titleLength-len(title)) +
		stylesheet.Pip(uint(m.selected), uint(num))
	if required {
		left += stylesheet.RequiredTitle(title)
	} else {
		left += stylesheet.OptionalTitle(title)
	}

	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, left+" ", view) + "\n")
}

// Reset junks all data in metadata, allowing it to be reused as if freshly created.
func (m *metadata) Reset() error {
	m.inputErr = ""
	m.submitErr = ""
	m.selected = 0

	m.name.Reset()
	m.description.Reset()
	m.tag.Reset()
	m.enable = false
	m.maxEvents.Reset()
	m.retain.Reset()
	return nil
}
