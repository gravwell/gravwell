package scaffoldedit

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
)

// stateEdit is the collection of fields required to track and display an item currently being edited.
// Expects to be prepared by editModel.enterEditMode().
type stateEdit[S any] struct {
	err              string // SetField or Update encountered an error or invalid setting
	item             S      // the item being altered
	longestLineWidth int    // longest line width

	// currently selected field.
	// Equal to len(tiCount), where the last item is the submit button
	selected uint
	// # of TIs currently available for editing this item
	tiCount     int
	orderedKTIs []KeyedTI // KTIs, sorted by rank (cfg.Order)
}

// update() handling for editing mode, used onces an item has been selected from the list of editables.
// Updates the TIs and performs data transmutation and submission if user confirms changes.
//
// An item identifier, as returned by updateSub, is returned iff the item update subroutine was triggered and processed successfully.
// The empty string means that an error occurred or the item update subroutine was not fired at all.
func (se *stateEdit[S]) update(msg tea.Msg, _ Config, setFieldSub SetFieldSubroutine[S], updateSub UpdateStructSubroutine[S]) (
	_ tea.Cmd, identifier string,
) {
	if _, ok := msg.(tea.KeyMsg); ok {
		se.err = "" // clear input errors on new key input
		switch {
		case hotkeys.IsSubmit(msg):
			if se.submitSelected() {
				var missing []string
				for _, kti := range se.orderedKTIs { // check all required fields are populated
					if kti.Required && strings.TrimSpace(kti.TI.Value()) == "" {
						missing = append(missing, kti.Key)
					}
				}

				// if fields are missing, warn and do not submit
				if len(missing) > 0 {
					imploded := strings.Join(missing, ", ")
					copula := "is"
					if len(missing) > 1 {
						copula = "are"
					}
					se.err = fmt.Sprintf("%v %v required", imploded, copula)
					return textinput.Blink, ""
				}

				// yank the TI values and reinstall them into a data structure to update against
				for _, kti := range se.orderedKTIs {
					if inv, err := setFieldSub(&se.item, kti.Key, kti.TI.Value()); err != nil {
						clilog.Writer.Errorf("failed to set value '%v' to field with key %v (item: %v)", kti.TI.Value(), kti.Key, se.item)
						se.err = err.Error()
						return nil, ""
					} else if inv != "" {
						se.err = inv
						return textinput.Blink, ""
					}
				}

				// perform the update
				identifier, err := updateSub(&se.item)
				if err != nil {
					se.err = err.Error()
					return textinput.Blink, ""
				}
				// success
				return nil, identifier
			} else {
				se.nextTI()
			}
		case hotkeys.IsCursorUp(msg):
			se.previousTI()
		case hotkeys.IsCursorDown(msg):
			se.nextTI()
		}
	}

	// update tis
	cmds := make([]tea.Cmd, len(se.orderedKTIs))
	for i, tti := range se.orderedKTIs {
		se.orderedKTIs[i].TI, cmds[i] = tti.TI.Update(msg)
	}
	return tea.Batch(cmds...), ""
}

func (se *stateEdit[S]) view() string {

	inputs := ViewKTIs(uint(se.longestLineWidth)/2, uint(se.longestLineWidth)/2, se.orderedKTIs, se.selected)

	var wrapSty = lipgloss.NewStyle().Width(se.longestLineWidth)

	var inE string
	if se.err != "" {
		inE = wrapSty.Render(se.err)
	}

	return inputs +
		"\n" +
		lipgloss.NewStyle().Width(lipgloss.Width(inputs)).AlignHorizontal(lipgloss.Center).Render(
			stylesheet.ViewSubmitButton(se.submitSelected(), se.longestLineWidth, inE),
		)
}

var (
	rightAlignSty = lipgloss.NewStyle().AlignHorizontal(lipgloss.Right)
)

// ViewKTIs composes a uniform view of the given keyedTIs.
// All field will be padded to a consistent length based on maxFieldWidth and right-aligned.
// TIs are attached as View() to their respective TIs.
func ViewKTIs(maxFieldWidth, maxTIWidth uint, ktis []KeyedTI, selectedIdx uint) string {
	if maxFieldWidth == 0 {
		clilog.Writer.Warnf("field width is unset")
	} else if maxTIWidth == 0 {
		clilog.Writer.Warnf("TI width is unset")
	}

	var fields []string
	var TIs []string

	var sb strings.Builder // reused each cycle
	for i, kti := range ktis {
		// apply consistent left padding, then pip
		sb.WriteString(strings.Repeat(" ", int(max(maxFieldWidth, maxTIWidth))-len(kti.Title)) + stylesheet.Pip(selectedIdx, uint(i)))
		// colourize and attach title
		if kti.Required {
			sb.WriteString(stylesheet.RequiredTitle(kti.Title))
		} else {
			sb.WriteString(stylesheet.OptionalTitle(kti.Title))
		}
		// render the line and right-align it
		fields = append(fields, rightAlignSty.Render(sb.String()))
		sb.Reset()

		TIs = append(TIs, kti.TI.View())
	}

	// compose all fields
	f := lipgloss.JoinVertical(lipgloss.Right, fields...)

	// compose all TIs
	t := lipgloss.JoinVertical(lipgloss.Left, TIs...)

	// conjoin fields and TIs
	return lipgloss.JoinHorizontal(lipgloss.Center, f, t)
}

// Blur existing TI, select and focus previous (higher) TI.
// Wraps from the first TI to the submit button.
func (se *stateEdit[S]) previousTI() {
	// if we are not on the submit button, then blur
	if !se.submitSelected() {
		se.orderedKTIs[se.selected].TI.Blur()
	}
	if se.selected == 0 { // wrap to submit button
		se.selected = uint(len(se.orderedKTIs))
	} else {
		se.selected -= 1
	}
	// if we are not on the submit button, then focus
	if !se.submitSelected() {
		se.orderedKTIs[se.selected].TI.Focus()
	}
}

// Blur existing TI, select and focus next (lower) TI.
// Selects the submit button after the last TI and wraps after the submit button.
func (se *stateEdit[S]) nextTI() {
	if !se.submitSelected() {
		se.orderedKTIs[se.selected].TI.Blur()
	}
	se.selected += 1
	if se.selected > uint(len(se.orderedKTIs)) { // jump to start
		se.selected = 0
	}
	if !se.submitSelected() {
		se.orderedKTIs[se.selected].TI.Focus()
	}
}

func (se *stateEdit[S]) reset() {
	var zero S

	se.orderedKTIs = nil
	se.selected = 0
	se.item = zero
	se.err = ""
	se.tiCount = 0
}

func (se *stateEdit[S]) submitSelected() bool {
	return se.selected == uint(se.tiCount)
}
