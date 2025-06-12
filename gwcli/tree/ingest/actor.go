/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"fmt"
	"net/netip"
	"os"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/busywait"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/colorizer"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/pflag"
)

type mode = string

const (
	picking   mode = "picking"
	ingesting mode = "ingesting"
	done      mode = "done"
)

var Ingest action.Model = Initial()

type ingest struct {
	fp          filepicker.Model
	mode        mode
	err         error
	ingestResCh chan error

	// modifier pane items
	modFocused bool            // is the modifier pane in focus?
	tagTI      textinput.Model // tag to ingest file under
	srcTI      textinput.Model // user-provided IP address source
	ignoreTS   bool
	localTime  bool

	spinner spinner.Model // TODO should busywait be pushed into stylesheet?
}

func Initial() *ingest {
	i := &ingest{
		fp:          filepicker.New(),
		mode:        picking,
		ingestResCh: make(chan error),

		srcTI: stylesheet.NewTI("", true),
		tagTI: stylesheet.NewTI("default", true),
	}

	i.srcTI.Placeholder = "127.0.0.1"

	return i
}

func (i *ingest) Update(msg tea.Msg) tea.Cmd {
	switch i.mode {
	case done: // wait for mother to take over
		return nil
	case ingesting: // wait for results
		// check if we are done
		select {
		case err := <-i.ingestResCh:
			i.mode = done
			// print the error above the TUI; the other goro logs for us
			if err != nil {
				return tea.Println(stylesheet.ErrStyle.Render(err.Error()))
			}
			return nil
		default:
			return i.spinner.Tick
		}
	default: //case picking:
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			i.err = nil
			switch keyMsg.Type {
			case tea.KeyTab:
				// switch focus
				i.modFocused = !i.modFocused
				return nil
			case tea.KeyEnter:
				// check that src is empty or a valid IP
				src := i.srcTI.Value()
				if src != "" {
					if _, err := netip.ParseAddr(src); err != nil {
						// set error and return
						i.err = err
						return nil
					}
				}

				// check that a file has been selected
				file := i.fp.FileSelected
				tag := i.tagTI.Value()

				i.mode = ingesting

				// spin ingestion off into goroutine
				clilog.Writer.Warnf("ingesting file %v with parameters: tag='%v' src='%v' ignore=%v local=%v",
					file, tag, src, i.ignoreTS, i.localTime)
				go func() {
					resp, err := connection.Client.IngestFile(file, tag, src, i.ignoreTS, i.localTime)
					if err != nil {
						clilog.Writer.Warnf("failed to ingest file %v: %v", file, err)
						// spit above TUI and boot the user back out
					}
					clilog.Writer.Infof("successfully ingested file %v: %+v", file, resp)
				}()

				// start a spinner and wait
				i.spinner = busywait.NewSpinner()
			}

		}

		return nil
	}
}

func (i *ingest) View() string {
	// build modifier view
	modView := fmt.Sprintf("Ignore Timestamps? %v\t"+
		"Use Server Local Time? %v\t"+
		"source: %s\t"+
		"tag: %s",
		colorizer.Checkbox(i.ignoreTS),
		colorizer.Checkbox(i.localTime),
		i.srcTI.View(),
		i.tagTI.View())

	// TODO add spinner and second TI

	var spnrErrHelp string
	if i.mode == ingesting {
		spnrErrHelp = i.spinner.View()
	} else if i.err != nil {
		spnrErrHelp = stylesheet.ErrStyle.Render(i.err.Error())
	} else {
		// TODO
		spnrErrHelp = "" // display help keys for submission and changing focus
	}

	// wrap it in a border
	if i.modFocused {
		modView = stylesheet.Composable.Focused.Render(modView)
	} else {
		modView = stylesheet.Composable.Unfocused.Render(modView)
	}

	// compose views
	return lipgloss.JoinVertical(lipgloss.Center, i.fp.View(), modView, spnrErrHelp)
}

func (i *ingest) Done() bool {
	return i.mode == done
}

func (i *ingest) Reset() error {
	//i.fp // TODO does this need to be reset?
	i.mode = picking
	i.err = nil

	i.modFocused = false
	i.tagTI.Reset()
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

	// if one+ files were given, try to ingest immediately
	// TODO

	// update the spinning view to just declare the file(s) that are being ingested

	return "", i.fp.Init(), nil
}
