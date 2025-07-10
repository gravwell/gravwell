/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

/*
Interactive usage currently only supports selecting a single file each invokation due to limitations in the filepicker bubble.
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

const maxPickerHeight int = 50

type mode = string

const (
	picking   mode = "picking"   // user is selecting an item to upload
	ingesting mode = "ingesting" // a file has been selected and is being uploaded
	done      mode = "done"
)

// ensure we satisfy the action interface
var _ action.Model = Initial()

type ingest struct {
	boxWidth int // width of the filepicker view, inside of its box

	width       int // current known maximum width of the terminal
	height      int // current known maximum height of the terminal
	mode        mode
	err         error // error displayed under file picker; cleared on key entry
	ingestResCh chan struct {
		string
		error
	}
	ingestCount int // the number of files to wait for in ingesting mode (from ingestResCh)

	mod mod // modifier pane

	spinner spinner.Model

	fg filegrabber.FileGrabber // mildly upgraded filepicker
}

// Initial returns a pointer to a new ingest action.
// It is ready for use/.SetArgs().
func Initial() *ingest {
	i := &ingest{
		fg:   filegrabber.New(true, false),
		mode: picking,
		ingestResCh: make(chan struct {
			string
			error
		}),

		mod: NewMod(),
	}
	i.fg.Cursor = stylesheet.Cur.PromptSty.Symbol()
	i.fg.DirAllowed = false
	i.fg.FileAllowed = true
	i.fg.ShowSize = true

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
			return resultCmd
		default: // no results ready, just spin
			return i.spinner.Tick
		}
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
		// intercept window size messages
		if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
			i.width = wsMsg.Width
			// need to save off 3 cells
			// left border (1) +
			// right border (1) +
			// center divider btwn file names and size/permissions (1)
			inner := wsMsg.Width - 3
			// left side (pip+permissions+size) and right size (file/dir name) should each be half of the remainder
			// NOTE(rlandau): this is not used as we can only view a filepicker as a complete unit;
			// we can not compose the left and right individually until we implement pathbasket.

			i.boxWidth = inner
			i.fg.Styles.File = i.fg.Styles.File.MaxWidth(inner)
			i.fg.Styles.Selected = i.fg.Styles.Selected.MaxWidth(inner)
			i.fg.Styles.Symlink = i.fg.Styles.Symlink.MaxWidth(inner)
			i.fg.Styles.Directory = i.fg.Styles.Directory.MaxWidth(inner)
			i.fg.Styles.DisabledFile = i.fg.Styles.DisabledFile.MaxWidth(inner)
			i.fg.Styles.DisabledSelected = i.fg.Styles.DisabledSelected.MaxWidth(inner)

			i.height = wsMsg.Height
		}

		// pass message to mod view or fp, depending on focus
		var cmd tea.Cmd
		if i.mod.focused {
			i.mod, cmd = i.mod.update(msg)
		} else {
			i.fg, cmd = i.fg.Update(msg)
			// check for file selection (and thus, attempt ingestion)
			if didSelect, path := i.fg.DidSelectFile(msg); didSelect {
				// validate selections and modifiers prior to ingestion
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

				tag := strings.TrimSpace(i.mod.tagTI.Value())
				var err error
				tag, err = determineTag(path, tag, "")
				if err != nil {
					if errors.Is(err, errNoTagSpecified) {
						i.err = errors.New("a tag must be specified for this file")
					} else {
						i.err = err
					}
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
			if didSelect, path := i.fg.DidSelectDisabledFile(msg); didSelect {
				// Let's clear the selectedFile and display an error.
				i.err = errors.New(path + " is not a valid file for ingestion")
				return nil
			}
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
	availWidth := i.width - (stylesheet.Cur.ComposableSty.ComplimentaryBorder.GetHorizontalMargins() +
		stylesheet.Cur.ComposableSty.ComplimentaryBorder.GetHorizontalPadding() +
		2) // ensure we have at least a cell on either side
	path := i.fg.CurrentDirectory
	if availWidth < 0 {
		return path
	}
	// determine if we need to truncate
	if overflowing := len(path) - availWidth; overflowing > 0 {
		path = path[overflowing:] // left trim the overflow
	}

	return stylesheet.Cur.ComposableSty.ComplimentaryBorder.Render(path)
}

func (i *ingest) pickerView() string {
	// NOTE(rlandau): Width > maxWidth is a hacky way to ensure file names are truncated, not wrapped.
	sty := lipgloss.NewStyle().MaxWidth(i.boxWidth).Width(i.boxWidth + 100)

	// figure out how much height everything else needs
	breadcrumbHeight := lipgloss.Height(stylesheet.Cur.ComposableSty.ComplimentaryBorder.Render(i.fg.CurrentDirectory))
	modHeight := lipgloss.Height(i.mod.view(i.width))
	errHelpHeight := lipgloss.Height(i.errHelpView())
	buffer := 5

	newHeight := i.height - (breadcrumbHeight + modHeight + errHelpHeight + buffer)
	i.fg.SetHeight(min(newHeight, maxPickerHeight))

	var s = lipgloss.JoinVertical(lipgloss.Center, sty.Render(i.fg.View()), sty.Render(i.errHelpView()))
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
		return i.fg.ViewHelp() // display help keys for submission and changing focus
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

	i.fg.Reset()

	return nil
}

// SetArgs places the filepicker in the user's pwd and sets defaults based on flag.
func (i *ingest) SetArgs(fs *pflag.FlagSet, tokens []string) (string, tea.Cmd, error) {
	var err error

	rawFlags := initialLocalFlagSet()
	rawFlags.AddFlagSet(fs)
	if err := rawFlags.Parse(tokens); err != nil {
		return "", nil, err
	}
	flags, invalids, err := transmogrifyFlags(&rawFlags)
	if err != nil {
		return "", nil, err
	}
	if len(invalids) > 0 {
		// concatenate invalids and return them
		var full strings.Builder
		for _, reason := range invalids {
			full.WriteString(reason + "\n")
		}
		return full.String(), nil, nil
	}

	pairs := parsePairs(rawFlags.Args())

	// if one+ files were given, try to ingest immediately
	if len(pairs) > 0 {
		count := autoingest(i.ingestResCh, flags, pairs)
		if count == 0 {
			// should be impossible
			panic("autoingest returned a count of 0")
		}
		i.ingestCount = len(pairs)
		i.mode = ingesting
		return "", i.spinner.Tick, nil
	}

	// prepare the interactive action
	i.mod.tagTI.SetValue(flags.defaultTag)
	i.mod.srcTI.SetValue(flags.src)

	if flags.dir != "" {
		i.fg.CurrentDirectory = flags.dir
	}

	return "", i.fg.Init(), nil
}
