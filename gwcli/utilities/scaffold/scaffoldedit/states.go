package scaffoldedit

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
)

// stateEdit is the collection of fields required to track and display an item currently being edited.
type stateEdit[S any] struct {
	err string // SetField or Update encountered an error or invalid setting

	// currently selected field.
	// Equal to len(tiCount), where the last item is the submit button
	hovered uint
	item    S // the item being altered

	tiCount     int                // # of TIs currently available for editing this item
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
	var sb strings.Builder
	for _, kti := range se.orderedKTIs {
		// color the title appropriately
		if kti.Required {
			sb.WriteString(tiFieldRequiredSty.Render(kti.Key + ": "))
		} else {
			sb.WriteString(tiFieldOptionalSty.Render(kti.Key + ": "))
		}
		sb.WriteString(kti.TI.View() + "\n")
	}
	//sb.WriteString(stylesheet.SubmitString("alt+enter", em.inputErr, em.updateErr, em.width))
	//sb.WriteString(stylesheet.ViewSubmitButton(em.sel, em.inputErr, em.updateErr, em.width))
	return sb.String()
}

// Blur existing TI, select and focus previous (higher) TI
func (se *stateEdit[S]) previousTI() {
	se.orderedKTIs[se.hovered].TI.Blur()
	if se.hovered == 0 {
		se.hovered = uint(se.tiCount - 1)
	} else {
		se.hovered -= 1
	}
	se.orderedKTIs[se.hovered].TI.Focus()
}

// Blur existing TI, select and focus next (lower) TI
func (se *stateEdit[S]) nextTI() {
	se.orderedKTIs[se.hovered].TI.Blur()
	se.hovered += 1
	if se.hovered >= uint(se.tiCount) {
		se.hovered = 0
	}
	se.orderedKTIs[se.hovered].TI.Focus()
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
