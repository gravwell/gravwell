package alertscreate

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/validate"
)

// fieldnum identifies each field numerically so we can figure out which one is currently selected
type fieldNum uint

const (
	metaName fieldNum = iota
	metaDescription
	metaTag
	metaEnable
	metaMaxEvents
	metaRetain
	metaContinue
)

type metadata struct {
	inputErr string // a validation error from one of the below inputs
	selected fieldNum

	// required

	name textinput.Model

	// optional

	description textinput.Model
	tag         textinput.Model
	enable      bool
	maxEvents   textinput.Model // convert to int on submit
	retain      textinput.Model // read as time.Duration, converted to seconds on submit
}

func NewMetadata() *metadata {
	m := &metadata{
		name:        stylesheet.NewTI("", false),
		description: stylesheet.NewTI("", true),
		tag:         stylesheet.NewTI("", true),
		maxEvents:   stylesheet.NewTI("", true),
		retain:      stylesheet.NewTI("", true),
	}

	m.tag.Placeholder = "_alerts"
	m.maxEvents.Validate = validate.Numeric
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
	m.Reset()
	return m
}

const titleLength = len("description") + 1 // compose titles based on the longest title +1 (additional left pad)

// Init sets initial values into metadata.
// It is safe to use metadata without Init, but good practice to call it just in case.
func (m *metadata) Init(name, description, tag string, enable bool, maxEvents int, retainS int32) {
	m.name.SetValue(name)
	m.name.Focus()
	m.description.SetValue(description)
	m.tag.SetValue(tag)
	m.enable = enable
	m.maxEvents.SetValue(strconv.FormatInt(int64(maxEvents), 10))
	if retainS != 0 {
		m.retain.SetValue(strconv.FormatInt(int64(retainS), 10) + "s")
	}
	m.checkSatisfaction()
}

func (m *metadata) Update(msg tea.Msg) (_ tea.Cmd, done bool) {
	if _, ok := msg.(tea.KeyMsg); ok {
		// check for vertical movement
		if handled, _, newIndex := hotkeys.MoveCursor(msg, uint(m.selected), uint(metaContinue)+1, nil); handled {
			m.toggleFocus(false)
			m.selected = fieldNum(newIndex)
			m.toggleFocus(true)
			return textinput.Blink, false
		}

		if hotkeys.Match(msg, hotkeys.Select) && m.selected == metaEnable {
			m.enable = !m.enable
		} else if hotkeys.ButtonPressed(msg) &&
			m.selected == metaContinue {
			if m.inputErr != "" {
				return nil, false
			}
			// all done!
			return nil, true
		}
	}

	// pass the message into the appropriate text input
	defer m.checkSatisfaction()

	var cmd tea.Cmd
	switch m.selected {
	case metaName:
		m.name, cmd = m.name.Update(msg)
	case metaDescription:
		m.description, cmd = m.description.Update(msg)
	case metaTag:
		m.tag, cmd = m.tag.Update(msg)
	case metaMaxEvents:
		m.maxEvents, cmd = m.maxEvents.Update(msg)
	case metaRetain:
		m.retain, cmd = m.retain.Update(msg)
	}
	return cmd, false
}

func (m *metadata) checkSatisfaction() {
	if m.name.Value() == "" {
		m.inputErr = phrases.MissingRequiredFields([]string{"Name"})
		return
	}
	if m.name.Err != nil {
		m.inputErr = m.name.Err.Error()
		return
	}
	if m.description.Err != nil {
		m.inputErr = m.description.Err.Error()
		return
	}
	if m.tag.Err != nil {
		m.inputErr = m.tag.Err.Error()
		return
	}
	if m.maxEvents.Err != nil {
		m.inputErr = m.maxEvents.Err.Error()
		return
	}
	if m.retain.Err != nil {
		m.inputErr = m.retain.Err.Error()
		return
	}
	m.inputErr = ""
}

// toggleFocus toggles the focus on the currently selected input (doing nothing if a non-TI/TA is selected).
// If !focus, blurs the input.
func (m *metadata) toggleFocus(focus bool) {
	switch m.selected {
	case metaName:
		if focus {
			m.name.Focus()
		} else {
			m.name.Blur()
		}
	case metaDescription:
		if focus {
			m.description.Focus()
		} else {
			m.description.Blur()
		}
	case metaTag:
		if focus {
			m.tag.Focus()
		} else {
			m.tag.Blur()
		}
	case metaMaxEvents:
		if focus {
			m.maxEvents.Focus()
		} else {
			m.maxEvents.Blur()
		}
	case metaRetain:
		if focus {
			m.retain.Focus()
		} else {
			m.retain.Blur()
		}
	case metaEnable, metaContinue:
	default:
		s := "focus"
		if !focus {
			s = "blur"
		}
		clilog.Writer.Errorf("failed to %s input: unknown field number %v selected", s, m.selected)
	}
}

func (m *metadata) View() string {
	var sb strings.Builder

	m.viewline(&sb, true, "Name", metaName, m.name.View())
	m.viewline(&sb, false, "Description", metaDescription, m.description.View())
	m.viewline(&sb, false, "Tag", metaTag, m.tag.View())
	m.viewline(&sb, false, "Enable", metaEnable, stylesheet.Checkbox(m.enable))
	m.viewline(&sb, false, "Max Events", metaMaxEvents, m.maxEvents.View())
	m.viewline(&sb, false, "Retain", metaRetain, m.retain.View())

	sb.WriteString(stylesheet.ViewSubmitLikeButton("continue", m.selected == metaContinue, titleLength*2, m.inputErr))

	sb.WriteByte('\n')
	sb.WriteString(hotkeys.DefaultView(titleLength*2))
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

	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, left+" ", view))
	sb.WriteByte('\n')
}

// Reset junks all data in metadata, allowing it to be reused as if freshly created.
func (m *metadata) Reset() error {
	m.inputErr = ""
	m.selected = 0

	m.name.Reset()
	m.name.Focus()
	m.description.Reset()
	m.tag.Reset()
	m.enable = false
	m.maxEvents.Reset()
	m.retain.Reset()
	return nil
}
