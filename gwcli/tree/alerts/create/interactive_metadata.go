package alertscreate

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
)

type metadata struct {

	// required

	name textinput.Model

	// optional

	description textarea.Model
	tag         textinput.Model
	enable      bool
	maxEvents   textinput.Model // convert to int on submit
	retain      textinput.Model // read as time.Duration, converted to seconds on submit
}

const titleLength = len("description") + 1 // compose titles based on the longest title +1 (additional left pad)

func (m *metadata) Init(name, description, tag string, enable bool, maxEvents int, retainS int32) {
	m.name.SetValue(name)
	m.description.SetValue(description)
	m.description.SetValue(tag)
	m.enable = enable
	m.maxEvents.SetValue(strconv.FormatInt(int64(maxEvents), 10))
	m.retain.SetValue(strconv.FormatInt(int64(retainS), 10))
}

func (m *metadata) Update() tea.Cmd {

}

func (m *metadata) View() string {
	var sb strings.Builder

	// name
	sb.WriteString(strings.Repeat(" ", titleLength-len("name")) + stylesheet.RequiredTitle("Name"))
	sb.WriteString(m.name.View())
	sb.WriteRune('\n')

	// description
	sb.WriteString(strings.Repeat(" ", titleLength-len("description")) + stylesheet.OptionalTitle("Description"))
	sb.WriteString(m.description.View())
	sb.WriteRune('\n')

	// tag
	sb.WriteString(strings.Repeat(" ", titleLength-len("enable")) + stylesheet.OptionalTitle("Enable"))
	sb.WriteString(stylesheet.Checkbox(m.enable))
	sb.WriteRune('\n')

	// enable
	sb.WriteString(strings.Repeat(" ", titleLength-len("max events")) + stylesheet.OptionalTitle("Max Events"))
	sb.WriteString(m.maxEvents.View())
	sb.WriteRune('\n')

	// retain
	sb.WriteString(strings.Repeat(" ", titleLength-len("retain")) + stylesheet.OptionalTitle("Retain"))
	sb.WriteString(m.retain.View())
	sb.WriteRune('\n')

	return sb.String()
}

func (m *metadata) Reset() error {
	m.name.Reset()
	m.description.Reset()
	m.tag.Reset()
	m.enable = false
	m.maxEvents.Reset()
	m.retain.Reset()
	return nil
}
