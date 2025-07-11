package datascope

import (
	"testing"

	"github.com/charmbracelet/x/exp/teatest"
	grav "github.com/gravwell/gravwell/v4/client"
)

// TODO can this be moved to a ci compatible test file?
func Test_Tea(t *testing.T) {

	// create some dummy data
	data := []string{
		"Line 1",
		"Multi\nLine2",
		"Line 3",
	}
	// create a dummy search that should work so long as we don't trigger download or schedule
	search := grav.Search{RenderMod: "text"}

	ds, cmd, err := NewDataScope(data, false, &search, false)
	if err != nil {
		t.Fatalf("failed to create datascope: %v", err)
	} else if cmd != nil {
		t.Fatalf("datascope should never return a command if it knows Mother isn't running. Returned command: %v", err)
	}
	// spin up the teatest
	tm := teatest.NewTestModel(t, ds, teatest.WithInitialTermSize(300, 100))
}
