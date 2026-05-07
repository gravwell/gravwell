package scaffoldedit

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
)

// stateEdit is the collection of fields required to track and display an item currently being edited.
// Expects to be prepared by editModel.enterEditMode().
type stateEdit[S any] struct {
	err          string // SetField or Update encountered an error or invalid setting
	item         S      // the item being altered
	longestWidth int    // longest line width

	// currently selected field.
	// Equal to len(tiCount), where the last item is the submit button
	hovered uint
	// # of TIs currently available for editing this item
	tiCount     int
	orderedKTIs []scaffold.KeyedTI // KTIs, sorted by rank (cfg.Order)
}

// Update() handling for editing mode.
// Updates the TIs and performs data transmutation and submission if user confirms changes. // TODO
//
// identifier returns iff the updateSubr was triggered and processed successfully.
// The empty string means that an error occurred or updateSubr was not fired at all.
func (se *stateEdit[S]) update(msg tea.Msg,
	cfg Config,
	setFieldSub SetFieldSubroutine[S],
	updateSub UpdateStructSubroutine[S]) (_ tea.Cmd, identifier string) {
	if keymsg, ok := msg.(tea.KeyMsg); ok {
		se.err = "" // clear input errors on new key input
		switch keymsg.Type {
		case tea.KeyEnter:
			if se.submitHovered() {
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
		case tea.KeyUp:
			se.previousTI()
		case tea.KeyDown:
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
	inputs := scaffold.ViewKTIs(uint(se.longestWidth), se.orderedKTIs, se.hovered)

	var wrapSty = lipgloss.NewStyle().Width(se.longestWidth)

	var inE string
	if se.err != "" {
		inE = wrapSty.Render(se.err)
	}

	return inputs +
		"\n" +
		lipgloss.NewStyle().Width(lipgloss.Width(inputs)).AlignHorizontal(lipgloss.Center).Render(
			stylesheet.ViewSubmitButton(se.submitHovered(), inE, ""),
		)
}

// Blur existing TI, select and focus previous (higher) TI.
// Wraps from the first TI to the submit button.
func (se *stateEdit[S]) previousTI() {
	// if we are not on the submit button, then blur
	if !se.submitHovered() {
		se.orderedKTIs[se.hovered].TI.Blur()
	}
	if se.hovered == 0 { // wrap to submit button
		se.hovered = uint(len(se.orderedKTIs))
	} else {
		se.hovered -= 1
	}
	// if we are not on the submit button, then focus
	if !se.submitHovered() {
		se.orderedKTIs[se.hovered].TI.Focus()
	}
}

// Blur existing TI, select and focus next (lower) TI.
// Selects the submit button after the last TI and wraps after the submit button.
func (se *stateEdit[S]) nextTI() {
	if !se.submitHovered() {
		se.orderedKTIs[se.hovered].TI.Blur()
	}
	se.hovered += 1
	if se.hovered > uint(len(se.orderedKTIs)) { // jump to start
		se.hovered = 0
	}
	if !se.submitHovered() {
		se.orderedKTIs[se.hovered].TI.Focus()
	}
}

func (se *stateEdit[S]) reset() {
	var zero S

	se.orderedKTIs = nil
	se.hovered = 0
	se.item = zero
	se.err = ""
	se.tiCount = 0
}

func (se *stateEdit[S]) submitHovered() bool {
	return se.hovered == uint(se.tiCount)
}
