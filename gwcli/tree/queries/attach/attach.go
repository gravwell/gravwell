/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package attach implements search re-attachment, for fetching backgrounded queries.
// It bears significant similarities to the load-bearing query action, but is different enough to not be folded in.
package attach

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/querysupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	helpDesc string = "" // TODO
)

// NewAttachAction creates an attach action of the form `./gwcli ... attach 123456789`
func NewAttachAction() action.Pair {
	cmd := treeutils.GenerateAction(
		"attach",
		"re-attach to a backgrounded query",
		helpDesc,
		[]string{"reattach"},
		run)

	localFS := initialLocalFlagSet()
	cmd.Flags().AddFlagSet(&localFS)
	// add bare argument validator (require 1 arg if --script, 0 or 1 otherwise)
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		script, err := cmd.Flags().GetBool("script")
		if err != nil {
			panic(err)
		}
		if script && len(args) != 1 {
			return errors.New("attach requires exactly 1 argument in script mode.\n" + syntax(cmd, true))
		}
		if len(args) > 1 {
			return errors.New("attach takes 0 or 1 argument in interactive mode.\n" + syntax(cmd, false))
		}
		return nil
	}
	cmd.Example = "gwcli queries attach 123456789"

	return action.NewPair(cmd, Attach)
}

// Generates the flagset used by attach.
// In interactive mode, most flags are passed directly into datascope.
func initialLocalFlagSet() pflag.FlagSet {
	fs := pflag.FlagSet{}

	fs.StringP(ft.Name.Output, "o", "", ft.Usage.Output)
	fs.Bool(ft.Name.Append, false, ft.Usage.Append)
	fs.Bool(ft.Name.JSON, false, ft.Usage.JSON)
	fs.Bool(ft.Name.CSV, false, ft.Usage.CSV)

	return fs
}

// invoked from the commandline.
// Invokes Mother if !script.
func run(cmd *cobra.Command, args []string) {
	var err error

	// fetch flags
	flags, err := transmogrifyFlags(cmd.Flags())
	if err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		return
	}

	// attempt to fetch the search id
	var sid string
	if len(args) == 1 {
		sid = strings.TrimSpace(args[0])
	}

	// split on --script
	if flags.script {
		// arg validator should ensure sid is populated by this point

		// attaching to a search non-interactively just downloads the results and spits them out
		s, err := connection.Client.AttachSearch(sid)
		if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
			return
		}
		defer s.Close()

		rc, format, err := connection.DownloadSearch(&s, types.TimeRange{}, flags.csv, flags.json)
		if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
			return
		}
		if err := querysupport.WriteDownloadResults(rc, cmd.OutOrStdout(), flags.outfn, flags.append, format); err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), connection.DownloadQuerySuccessfulString(flags.outfn, flags.append, format))
		}

		return
	}

	// if a sid was given, attempt to go directly to datascope
	// TODO

	// if a sid was not given, launch Mother into bare `attach` call
	// TODO

}

// logical struct to store flag data after verifying flag construction
// TODO should this be consolidated with query's into utilities?
type queryflags struct {
	script bool
	json   bool
	csv    bool
	outfn  string
	append bool
}

// transmogrifyFlags takes a *parsed* flagset and returns a structured, typed, and (in the case of
// strings) trimmed representation of the flags therein.
// If an error occurs, the current state of the flags will be returned, but may be incomplete.
// While it will coerce data to an appropriate type, transmogrify will *not* check for the state of
// required or dependent flags.
func transmogrifyFlags(fs *pflag.FlagSet) (queryflags, error) {
	var (
		err error
		qf  queryflags
	)

	if qf.script, err = fs.GetBool(ft.Name.Script); err != nil {
		// this will fail if mother is running, it is okay to swallow
		qf.script = false
	}
	if qf.json, err = fs.GetBool(ft.Name.JSON); err != nil {
		return qf, err
	}
	if qf.csv, err = fs.GetBool(ft.Name.CSV); err != nil {
		return qf, err
	}

	if qf.outfn, err = fs.GetString(ft.Name.Output); err != nil {
		return qf, err
	} else {
		qf.outfn = strings.TrimSpace(qf.outfn)
	}
	if qf.append, err = fs.GetBool(ft.Name.Append); err != nil {
		return qf, err
	}

	return qf, nil

}
