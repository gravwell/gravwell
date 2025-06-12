/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package ingest provides an action for streaming data TO Gravwell (as opposed to most other actions that operate in reverse).
package ingest

import (
	"fmt"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	helpDesc = "Ingest 1+ files into Gravwell.\n" +
		"All bare arguments after `ingest` will be considered file paths of files to slurp.\n" +
		"Calling ingest with no arguments will spin up a file picker (unless --script is specified in which case it will fail out)."
)

func NewIngestAction() action.Pair {
	cmd := treeutils.GenerateAction(
		"ingest",
		"ingest data from a file or STDIN",
		helpDesc, []string{"in", "sip", "read"}, run)
	cmd.Example = fmt.Sprintf("./gwcli ingest --tags=[\"pulsar\",\"quasar\",\"...\"] %s %s %s", ft.Mandatory("path1"), ft.Mandatory("path2"), ft.Mandatory("...")) // TODO

	{ // install flags
		fs := initialLocalFlagSet()
		cmd.Flags().AddFlagSet(&fs)
	}

	return action.NewPair(cmd, Ingest)

}

// Returns the set of flags expected by ingest.
func initialLocalFlagSet() pflag.FlagSet {
	fs := pflag.FlagSet{}

	fs.StringP("src", "s", "", "IP address to use as the source of these files")
	fs.Bool("ignore-timestamp", false, "all entries will be tagged with the current time")
	fs.Bool("local-time", false, "any timezone information in the data will be ignored and "+
		"timestamps will be assumed to be in the Gravwell server's local timezone")
	fs.StringSliceP("tags", "t", nil, "comma-separated tags to apply to a file/file=s.\n"+
		"If a single tag is specified, it will be applied to all files being ingested.\n"+
		"If multiple tags are specified, they will be matched index-for-index with the files given.")

	return fs
}

func run(c *cobra.Command, args []string) {
	// fetch flags
	script, err := c.Flags().GetBool(ft.Name.Script)
	if err != nil {
		clilog.Writer.Fatalf("script flag does not exist: %v", err)
	}
	tags, err := c.Flags().GetStringSlice("tags")
	if err != nil {
		clilog.Writer.Fatalf("local-time flag does not exist: %v", err)
	}
	// fetch list of files from the excess arguments
	files := c.Flags().Args()

	// if no file were given, launch mother or fail out
	if len(files) == 0 {
		if script {
			fmt.Fprintln(c.ErrOrStderr(), "at least one file path must be specified in script mode")
			return
		}

		if err := mother.Spawn(c.Root(), c, args); err != nil {
			clilog.Tee(clilog.CRITICAL, c.ErrOrStderr(),
				"failed to spawn a mother instance: "+err.Error()+"\n")
		}
		return
	}

	// check that tag len is 1 or == file len
	if len(tags) != 1 && len(tags) != len(files) {
		fmt.Fprintf(c.ErrOrStderr(), "tag count must be 1 or equal to the number of files specified (%v)", len(files))
		return
	}

	// try to ingest each file
	for i, f := range files {
		var tag string
		if len(tags) == 1 {
			tag = tags[0]
		} else {
			tag = tags[i]
		}

		ignoreTS, err := c.Flags().GetBool("ignore-timestamp")
		if err != nil {
			clilog.Writer.Fatalf("ignore-timestamp flag does not exist: %v", err)
			fmt.Println(uniques.ErrGeneric)
		}
		localTime, err := c.Flags().GetBool("local-time")
		if err != nil {
			clilog.Writer.Fatalf("local-time flag does not exist: %v", err)
			fmt.Println(uniques.ErrGeneric)

		}
		src, err := c.Flags().GetString("src")
		if err != nil {
			clilog.Writer.Fatalf("src flag does not exist: %v", err)
			fmt.Println(uniques.ErrGeneric)
		}

		resp, err := connection.Client.IngestFile(f, tag, src, ignoreTS, localTime)
		if err != nil {
			clilog.Tee(clilog.ERROR, c.ErrOrStderr(),
				"failed to ingest file "+f+":"+err.Error()+"\n")
			return
		}
		// spit out result if not script mode
		if !script {
			fmt.Fprintf(c.OutOrStdout(), "ingested file %v (size: %v) with tag %v", f, resp.Size, tag)
		}
	}
}
