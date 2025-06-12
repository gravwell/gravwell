package ingest

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/colorizer"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/pflag"
)

var Ingest action.Model = Initial()

type ingest struct {
	fp           filepicker.Model
	selectedFile string
	quitting     bool
	err          error

	// modifier pane items
	modFocused bool            // is the modifier pane in focus?
	srcTI      textinput.Model // user-provided IP address source
	ignoreTS   bool
	localTime  bool
}

func Initial() *ingest {
	i := &ingest{
		fp:    filepicker.New(),
		srcTI: stylesheet.NewTI("", true),
	}

	i.srcTI.Placeholder = "127.0.0.1"

	// TODO

	return i
}

func (i *ingest) Update(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyTab:
			// switch focus
			i.modFocused = !i.modFocused
			return nil
		case tea.KeyEnter:
			// check that src is empty or a valid IP
			// TODO
			// attempt ingestion
			// TODO
		}

	}

	return nil
}

func (i *ingest) View() string {
	// build modifier view
	modView := fmt.Sprintf("Ignore Timestamps? %v\t"+
		"Use Server Local Time? %v\t"+
		"source: %v",
		colorizer.Checkbox(i.ignoreTS),
		colorizer.Checkbox(i.localTime),
		i.srcTI.View())

	// wrap it in a border
	if i.modFocused {
		modView = stylesheet.Composable.Focused.Render(modView)
	} else {
		modView = stylesheet.Composable.Unfocused.Render(modView)
	}

	// compose views
	return lipgloss.JoinVertical(lipgloss.Center, i.fp.View(), modView)
}

func (i *ingest) Done() bool { return true }

func (i *ingest) Reset() error {
	//i.fp
	i.selectedFile = ""
	i.quitting = false
	i.err = nil

	i.modFocused = false
	i.srcTI.Reset()
	i.ignoreTS = false
	i.localTime = false

	return nil
}

// SetArgs places the filepicker in the user's pwd and sets defaults based on flag.
func (i *ingest) SetArgs(_ *pflag.FlagSet, tokens []string) (string, tea.Cmd, error) {
	var err error

	rawFlags := initialLocalFlagSet()
	if err := rawFlags.Parse(tokens); err != nil {
		return "", nil, err
	}

	// set default values by flags
	if i.ignoreTS, err = rawFlags.GetBool("ignore-timestamp"); err != nil {
		clilog.Writer.Fatalf("ignore-timestamp flag does not exist: %v", err)
		fmt.Println(uniques.ErrGeneric)
		return "", nil, err
	}
	if i.localTime, err = rawFlags.GetBool("local-time"); err != nil {
		clilog.Writer.Fatalf("local-time flag does not exist: %v", err)
		fmt.Println(uniques.ErrGeneric)
		return "", nil, err
	}
	if src, err := rawFlags.GetString("src"); err != nil {
		clilog.Writer.Fatalf("src flag does not exist: %v", err)
		return "", nil, err
	} else {
		i.srcTI.SetValue(src)
	}

	i.fp.CurrentDirectory, err = os.Getwd()
	if err != nil {
		clilog.Writer.Warnf("failed to get pwd: %v", err)
		i.fp.CurrentDirectory = "." // allow OS to decide where to drop us
	}

	return "", i.fp.Init(), nil
}
