/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

/*
Interactive usage currently on supports selecting a single file each invokation.
The module could be upgraded without too much trouble by adding a third pane (file picker, mod view, and selected files),
and altering `enter` to add the selected file to the list of selected.
Round it out by allowing users to interactive with the third pane to remove previously-selected files and viola.
*/

import (
	"fmt"
	"net/netip"
	"os"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
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
	ingestResCh chan struct {
		string
		error
	}
	ingestCount uint // the number of files to wait for in ingesting mode

	mod mod

	spinner spinner.Model // TODO should busywait be pushed into stylesheet?
}

func Initial() *ingest {
	i := &ingest{
		fp:   filepicker.New(),
		mode: picking,
		ingestResCh: make(chan struct {
			string
			error
		}),

		mod: NewMod(),
	}

	return i
}

func (i *ingest) Update(msg tea.Msg) tea.Cmd {
	switch i.mode {
	case done: // wait for mother to take over
		return nil
	case ingesting: // wait for results
		var resultCmd tea.Cmd
		select { // check for a result
		case res := <-i.ingestResCh:
			// spit the result above the current TUI
			if res.error == nil {
				resultCmd = tea.Printf("successfully ingested file %v", res.string)
			} else {
				s := fmt.Sprintf("failed to ingest file %v: %v", res.string, res.error)
				clilog.Writer.Warn(s)
				resultCmd = tea.Println(stylesheet.Sheet.ErrText.Render(s))
			}

			i.ingestCount -= 1
			if i.ingestCount == 0 { // all done
				i.mode = done
			}
		default: // no results ready, just spin
		}
		return tea.Batch(i.spinner.Tick, resultCmd)
	default: //case picking:
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			i.err = nil
			switch keyMsg.Type {
			case tea.KeyTab:
				// switch focus
				i.mod.focused = !i.mod.focused
				return nil
			case tea.KeyEnter:
				// check that src is empty or a valid IP
				src := i.mod.srcTI.Value()
				if src != "" {
					if _, err := netip.ParseAddr(src); err != nil {
						// set error and return
						i.err = err
						return nil
					}
				}

				// check that a file has been selected
				file := i.fp.FileSelected
				tag := i.mod.tagTI.Value()

				i.mode = ingesting

				// spin ingestion off into goroutine
				clilog.Writer.Warnf("ingesting file %v with parameters: tag='%v' src='%v' ignore=%v local=%v",
					file, tag, src, i.mod.ignoreTS, i.mod.localTime)
				go func() {
					_, err := connection.Client.IngestFile(file, tag, src, i.mod.ignoreTS, i.mod.localTime)
					i.ingestResCh <- struct {
						string
						error
					}{file, err}
				}()

				// start a spinner and wait
				i.spinner = stylesheet.NewSpinner()
				return nil
			}
		}
		// pass message to mod view or fp, depending on focus
		var cmd tea.Cmd
		if i.mod.focused {
			i.mod, cmd = i.mod.update(msg)
		} else {
			i.fp, cmd = i.fp.Update(msg)
		}
		return cmd
	}
}

func (i *ingest) View() string {
	switch i.mode {
	case done:
		return ""
	case ingesting: // display JUST a spinner; file statuses will be printed above the TUI for us
		return i.spinner.View()
	default:
		// generate the standard view:
		// breadcrumbs
		// file picker
		// modifiers
		// err-help

		var (
			breadcrumbs = stylesheet.Sheet.Composable.ComplimentaryBorder.Render(i.fp.CurrentDirectory)
			pickerView  string
			modView     = i.mod.view()
			errHelp     string
		)

		if i.err != nil {
			errHelp = stylesheet.Sheet.ErrText.Render(i.err.Error())
		} else {
			// TODO help keys
			errHelp = "" // display help keys for submission and changing focus
		}

		// wrap it in a border
		if i.mod.focused {
			pickerView = stylesheet.Sheet.Composable.UnfocusedBorder.Render(i.fp.View())
		} else {
			pickerView = stylesheet.Sheet.Composable.FocusedBorder.Render(i.fp.View())
		}

		// compose views
		return lipgloss.JoinVertical(lipgloss.Center, breadcrumbs, pickerView, modView, errHelp)
	}

}

func (i *ingest) Done() bool {
	return i.mode == done
}

func (i *ingest) Reset() error {
	//i.fp // TODO does this need to be reset?
	i.mode = picking
	i.err = nil

	i.mod = i.mod.reset()

	return nil
}

// SetArgs places the filepicker in the user's pwd and sets defaults based on flag.
func (i *ingest) SetArgs(_ *pflag.FlagSet, tokens []string) (string, tea.Cmd, error) {
	var err error

	rawFlags := initialLocalFlagSet()
	if err := rawFlags.Parse(tokens); err != nil {
		return "", nil, err
	}

	// fetch flag values
	if i.mod.ignoreTS, err = rawFlags.GetBool("ignore-timestamp"); err != nil {
		clilog.Writer.Fatalf("ignore-timestamp flag does not exist: %v", err)
		fmt.Println(uniques.ErrGeneric)
		return "", nil, err
	}
	if i.mod.localTime, err = rawFlags.GetBool("local-time"); err != nil {
		clilog.Writer.Fatalf("local-time flag does not exist: %v", err)
		fmt.Println(uniques.ErrGeneric)
		return "", nil, err
	}
	src, err := rawFlags.GetString("src")
	if err != nil {
		clilog.Writer.Fatalf("src flag does not exist: %v", err)
		return "", nil, err
	}
	tags, err := rawFlags.GetStringSlice("tags")
	if err != nil {
		clilog.Writer.Fatalf("src flag does not exist: %v", err)
		return "", nil, err
	}

	// if one+ files were given, try to ingest immediately
	if files := rawFlags.Args(); len(files) > 0 {
		ufErr := autoingest(i.ingestResCh, files, tags, i.mod.ignoreTS, i.mod.localTime, src)
		if ufErr != nil {
			return ufErr.Error(), nil, nil
		}
		i.ingestCount = uint(len(files))
		i.mode = ingesting
		return "", nil, nil
	}

	// prepare the action
	if len(tags) > 0 {
		i.mod.tagTI.SetValue(tags[0])
	}
	i.mod.srcTI.SetValue(src)

	i.fp.CurrentDirectory, err = os.Getwd()
	if err != nil {
		clilog.Writer.Warnf("failed to get pwd: %v", err)
		i.fp.CurrentDirectory = "." // allow OS to decide where to drop us
	}

	return "", i.fp.Init(), nil
}
