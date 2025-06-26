/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package ingest provides an action for streaming data TO Gravwell (as opposed to most other actions that operate in reverse).
// File paths and tags can be specified as bare arguments and have the form "path,tag", where tag and the splitting comma are optional (in some cases).
package ingest

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	helpDesc = "Ingest files into Gravwell.\n" +
		"An arbitrary number of arguments can be specified, each of which takes the form: " + ft.Mandatory("path") + ft.Optional(",tag") + "\n" +
		"If no flag is specified for a path, ingest will attempt to use the flag specified by --default-tag.\n" +
		"The path can point to a single file or a directory; if it is the latter, ingest will shallowly walk the directory to upload each immediate file (unless -r is specified, then it will traverse recursively).\n" +
		"Note, however, that ingest provides special handling for Gravwell JSON files.\n" +
		"Gravwell JSON files typically have a tag built into them, which will be used instead of --default-tag if a tag is not specified as part of the argument.\n" +
		"\n" +
		"Calling ingest with no arguments will spin up a file picker (unless --script is specified in which case it will fail out).\n" +
		"Use --dir to specify a starting directory (otherwise pwd will be used)."
)

// NewIngestAction does as it says on the tin, enabling the caller to insert the returned pair into the action map.
func NewIngestAction() action.Pair {
	cmd := treeutils.GenerateAction(
		"ingest",
		"ingest data from a file or STDIN",
		helpDesc, []string{"in", "sip", "read"}, run)
	cmd.Example = "./gwcli ingest picture/of/space.png,pulsar query_results.json cat/pics/,animals ..."

	{ // install flags
		fs := initialLocalFlagSet()
		cmd.Flags().AddFlagSet(&fs)
	}

	return action.NewPair(cmd, Ingest)

}

// Returns the set of flags expected by ingest.
func initialLocalFlagSet() pflag.FlagSet {
	fs := pflag.FlagSet{}

	fs.Bool("hidden", false,
		"include hidden files when ingesting a directory")
	fs.BoolP("recursive", "r", false,
		"recursively traverse directories, ingesting each file at every level")

	fs.StringP("source", "s", "",
		"IP address to use as the source of these files")
	fs.Bool("ignore-timestamp", false,
		"all entries will be tagged with the current time")
	fs.Bool("local-time", false,
		"any timezone information in the data will be ignored and "+
			"timestamps will be assumed to be in the Gravwell server's local timezone")
	fs.String("dir", "",
		"directory to start the interactive file picker in. Has no effect in script mode.")
	fs.StringP("default-tag", "t", "",
		"tag to use for each file that does not have one specified (either in the argument or embedded in the JSON (in the case of Gravwell JSON files))")

	return fs
}

// driver subroutine invoked by Cobra when ingest is called from an external shell.
// run boots Mother if !script && no files were specified; otherwise it attempts to autoingest the files.
func run(c *cobra.Command, args []string) {
	// fetch flags
	flags, invalids, err := transmogrifyFlags(c.Flags())
	if err != nil {
		fmt.Fprintf(c.ErrOrStderr(), "%v", err)
		return
	} else if len(invalids) > 0 { // spit out each invalid and die
		for _, reason := range invalids {
			fmt.Fprintln(c.ErrOrStderr(), reason)
		}
		return
	}

	// fetch pairs from bare arguments
	pairs := parsePairs(c.Flags().Args())

	// if no files were given, launch mother or fail out
	if len(pairs) == 0 {
		if flags.script {
			fmt.Fprintln(c.ErrOrStderr(), errNoFilesSpecified(true))
			return
		}

		if err := mother.Spawn(c.Root(), c, args); err != nil {
			clilog.Tee(clilog.CRITICAL, c.ErrOrStderr(),
				"failed to spawn a mother instance: "+err.Error()+"\n")
		}
		return
	}

	// attempt autoingestion

	resultCh := make(chan struct {
		string
		error
	})

	if err := autoingest(resultCh, flags, pairs); err != nil {
		fmt.Fprintln(c.ErrOrStderr(), stylesheet.Cur.ErrorText.Render(err.Error()))
		return
	}

	// start up a spinner
	var spinner *tea.Program
	if !flags.script {
		var s = "ingesting file"
		if len(pairs) > 1 {
			s += "s"
		}
		spinner = stylesheet.CobraSpinner(s)
		go func() { spinner.Run() }()
	}
	// print each result to stdout/stderr
	for range pairs {
		res := <-resultCh
		if res.error == nil {
			clilog.Tee(clilog.WARN, c.ErrOrStderr(), fmt.Sprintf("failed to ingest file '%v': %v\n", res.string, res.error))
		} else {
			fmt.Fprintf(c.OutOrStdout(), "successfully ingested file '%v'\n", res.string)
		}
	}
	// kill the spinner
	if spinner != nil {
		spinner.Kill()
	}

	/*
		done := make(chan bool) // close up shop, all files have been handled when closed

		go func() { // await results, print them, then notify us when all have been consumed
			for range files {
				res := <-resultCh
				if res.error != nil {
					clilog.Tee(clilog.WARN, c.ErrOrStderr(), fmt.Sprintf("failed to ingest file '%v': %v\n", res.string, res.error))
				} else {
					fmt.Fprintf(c.OutOrStdout(), "successfully ingested file '%v'\n", res.string)
				}
			}
			// all done
			close(done)
		}()

		if script { // wait
			<-done
		} else { // wait and display a spinner
			var s = "ingesting file"
			if len(files) > 1 {
				s += "s"
			}
			p := stylesheet.CobraSpinner(s)
			go func() { p.Run() }()
			<-done
			p.Quit()
		}

		// all done
	*/
}
