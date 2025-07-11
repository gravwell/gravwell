package killer_test

import (
	"math/rand"
	"testing"

	"github.com/Pallinder/go-randomdata"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	. "github.com/gravwell/gravwell/v4/gwcli/utilities/killer"
)

var gbl = GlobalKillKeys()
var chld = ChildKillKeys()

func TestCheckKillKeys(t *testing.T) {
	t.Run("global kill keys", func(t *testing.T) {
		for _, typ := range gbl {
			msg := tea.KeyMsg(tea.Key{
				Type:  typ,
				Runes: []rune{' '}, // bad practice, but shouldn't matter for this test
			})
			if CheckKillKeys(msg) != Global {
				t.Error("global kill key did not return a global kill enum")
			}

		}
	})

	t.Run("child kill keys", func(t *testing.T) {
		for _, typ := range chld {
			msg := tea.KeyMsg(tea.Key{
				Type:  typ,
				Runes: []rune{' '}, // bad practice, but shouldn't matter for this test
			})
			if CheckKillKeys(msg) != Child {
				t.Error("child kill key did not return a child kill enum")
			}

		}
	})

	t.Run("not a kill key", func(t *testing.T) {
		// generate some random key messages
		keyCount := 40
		var keys = make([]string, keyCount)
		for i := range keyCount {
			keys[i] = randomdata.RandStringRunes(rand.Intn(2) + 1)
		}

		for i := range keys {
			msg := tea.KeyMsg(tea.Key{
				Type:  tea.KeyRunes,
				Runes: []rune(keys[i]), // bad practice, but shouldn't matter for this test
			})
			if kill := CheckKillKeys(msg); kill != None {
				t.Error("non kill key returned a kill", testsupport.ExpectedActual(None, kill))
			}
		}
	})
	t.Run("not a key msg", func(t *testing.T) {
		msg := tea.WindowSizeMsg{Width: 300, Height: 100}
		if kill := CheckKillKeys(msg); kill != None {
			t.Error("non key message returned a kill", testsupport.ExpectedActual(None, kill))
		}
	})
}
