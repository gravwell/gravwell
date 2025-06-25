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
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/filegrabber"
	"github.com/spf13/pflag"
)

const maxHeight int = 50

type mode = string

const (
	picking   mode = "picking"
	ingesting mode = "ingesting"
	done      mode = "done"
)

var Ingest action.Model = Initial()

type ingest struct {
	width       int // current known maximum width of the terminal
	height      int
	mode        mode
	err         error
	ingestResCh chan struct {
		string
		error
	}
	ingestCount int // the number of files to wait for in ingesting mode

	mod mod

	spinner spinner.Model

	fp filegrabber.FileGrabber
}

func Initial() *ingest {
	i := &ingest{
		fp:   filegrabber.New(true, false),
		mode: picking,
		ingestResCh: make(chan struct {
			string
			error
		}),

		mod: NewMod(),
	}
	i.fp.AutoHeight = false // need to factor in other vertically-stacked elements
	i.fp.Cursor = stylesheet.Cur.PromptSty.Symbol()
	i.fp.DirAllowed = false
	i.fp.FileAllowed = true
	i.fp.ShowSize = true

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
				resultCmd = tea.Println(stylesheet.Cur.ErrorText.Render(s))
			}

			i.ingestCount -= 1
			if i.ingestCount <= 0 { // all done
				i.mode = done
			}
		default: // no results ready, just spin
		}
		return tea.Batch(i.spinner.Tick, resultCmd)
	default: //case picking:
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			i.err = nil
			// on tab, switch view
			if keyMsg.Type == tea.KeyTab || keyMsg.Type == tea.KeyShiftTab {
				// switch focus
				i.mod.focused = !i.mod.focused
				return textinput.Blink
			}
		}

		// pass message to mod view or fp, depending on focus
		var cmd tea.Cmd
		if i.mod.focused {
			i.mod, cmd = i.mod.update(msg)
		} else {
			i.fp, cmd = i.fp.Update(msg)
			// check for file selection (and thus, attempt ingestion)
			if didSelect, path := i.fp.DidSelectFile(msg); didSelect {
				if path == "" {
					i.err = errEmptyPath
					return cmd
				}
				// check that src is empty or a valid IP
				src := i.mod.srcTI.Value()
				if src != "" {
					if _, err := netip.ParseAddr(src); err != nil {
						// set error and return
						i.err = err
						return cmd
					}
				}

				tag := i.mod.tagTI.Value()
				if err := validateTag(tag); err != nil {
					i.err = err
					return cmd
				}

				i.ingestCount = 1
				i.mode = ingesting

				// spin ingestion off into goroutine
				clilog.Writer.Infof("ingesting file %v with parameters: tag='%v' src='%v' ignore=%v local=%v",
					path, tag, src, i.mod.ignoreTS, i.mod.localTime)
				go func() {
					_, err := connection.Client.IngestFile(path, tag, src, i.mod.ignoreTS, i.mod.localTime)
					i.ingestResCh <- struct {
						string
						error
					}{path, err}
				}()

				// start a spinner and wait
				i.spinner = stylesheet.NewSpinner()
				return tea.Batch(cmd, i.spinner.Tick)
			}

			// Did the user select a disabled file?
			// This is only necessary to display an error to the user.
			if didSelect, path := i.fp.DidSelectDisabledFile(msg); didSelect {
				// Let's clear the selectedFile and display an error.
				i.err = errors.New(path + " is not a valid file for ingestion")
				return nil
			}
		}

		// with all updates made, update sizes (if applicable)
		if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
			i.width = wsMsg.Width
			i.height = wsMsg.Height
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
		// compose views
		return lipgloss.JoinVertical(lipgloss.Center,
			i.breadcrumbsView(),
			i.pickerView(),
			i.mod.view(i.width),
		)
	}
}

//#region view helpers

func (i *ingest) breadcrumbsView() string {
	return stylesheet.Cur.ComposableSty.ComplimentaryBorder.Render(i.fp.CurrentDirectory)
}

func (i *ingest) pickerView() string {
	// generate the margins to ensure border stays stable during usage
	// split the width 3 ways
	usableWidth := i.width - 4
	leftMargin := (usableWidth / 4) + 5
	centerWidth := (usableWidth / 2)
	rightMargin := (usableWidth / 5)
	sty := lipgloss.NewStyle().
		MarginLeft(leftMargin).
		MarginRight(rightMargin).Width(centerWidth)

	// figure out how much height everything else needs
	breadcrumbHeight := lipgloss.Height(stylesheet.Cur.ComposableSty.ComplimentaryBorder.Render(i.fp.CurrentDirectory))
	modHeight := lipgloss.Height(i.mod.view(i.width))
	errHelpHeight := lipgloss.Height(i.errHelpView())
	buffer := 5

	newHeight := i.height - (breadcrumbHeight + modHeight + errHelpHeight + buffer)
	i.fp.SetHeight(min(newHeight, maxHeight))

	var s = lipgloss.JoinVertical(lipgloss.Center, sty.Render(i.fp.View()), i.errHelpView())
	if i.mod.focused {
		return stylesheet.Cur.ComposableSty.UnfocusedBorder.
			AlignHorizontal(lipgloss.Center).Render(s)
	} else {
		return stylesheet.Cur.ComposableSty.FocusedBorder.
			AlignHorizontal(lipgloss.Center).Render(s)
	}
}

func (i *ingest) errHelpView() string {
	if i.err != nil {
		return stylesheet.Cur.ErrorText.Render(i.err.Error())
	} else {
		return i.fp.ViewHelp() // display help keys for submission and changing focus
	}
}

//#endregion

func (i *ingest) Done() bool {
	return i.mode == done
}

func (i *ingest) Reset() error {
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
	flags, invalids, err := transmogrifyFlags(&rawFlags)
	if len(invalids) > 0 {
		var full strings.Builder
		// concatenate invalids
		for _, reason := range invalids {
			full.WriteString(reason + "\n")
		}
		return full.String(), nil, nil
	}

	pairs := parsePairs(rawFlags.Args())

	// if one+ files were given, try to ingest immediately
	if len(pairs) > 0 {
		ufErr := autoingest(i.ingestResCh, flags, pairs)
		if ufErr != nil {
			return ufErr.Error(), nil, nil
		}
		i.ingestCount = len(pairs)
		i.mode = ingesting
		return "", i.spinner.Tick, nil
	}

	// prepare the interactive action
	i.mod.tagTI.SetValue(flags.defaultTag)
	i.mod.srcTI.SetValue(flags.src.String())

	if flags.dir == "" {
		i.fp.CurrentDirectory, err = os.Getwd()
		if err != nil {
			clilog.Writer.Warnf("failed to get pwd: %v", err)
			i.fp.CurrentDirectory = "." // allow OS to decide where to drop us
		}
	} else {
		i.fp.CurrentDirectory = flags.dir
	}

	return "", i.fp.Init(), nil
}
