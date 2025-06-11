/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package extractors provides actions for interacting with autoextractors.
package extractors

import (
	"github.com/google/uuid"
	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// NewExtractorsNav returns a nav based around manipulating autoextractors.
func NewExtractorsNav() *cobra.Command {
	const (
		use   string = "extractors"
		short string = "manage your tag autoextractors"
		long  string = "Autoextractors describe how to extract fields from tagged, unstructured data."
	)

	var aliases = []string{"extractor", "ex", "ax", "autoextractor", "autoextractors"}

	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{},
		[]action.Pair{
			newExtractorsListAction(),
			newExtractorsCreateAction(),
			newExtractorDeleteAction()})
}

// #region list

func newExtractorsListAction() action.Pair {
	const (
		short string = "list extractors"
		long  string = "list autoextractions available to you and the system"
	)
	var defaultColumns = []string{"UID", "UUID", "Name", "Desc"}

	return scaffoldlist.NewListAction(
		"",
		short,
		long,
		defaultColumns,
		types.AXDefinition{},
		list,
		flags)
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.String("uuid", uuid.Nil.String(), "Fetches extraction by uuid.")
	return addtlFlags
}

func list(c *grav.Client, fs *pflag.FlagSet) ([]types.AXDefinition, error) {
	if id, err := fs.GetString("uuid"); err != nil {
		clilog.LogFlagFailedGet("uuid", err)
	} else {
		uid, err := uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		if uid != uuid.Nil {
			clilog.Writer.Infof("Fetching ax with uuid %v", uid)
			d, err := c.GetExtraction(id)
			return []types.AXDefinition{d}, err
		}
		// if uid was nil, move on to normal get-all
	}

	return c.GetExtractions()
}

//#endregion list
