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
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

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

	return scaffoldlist.NewListAction(
		short,
		long,
		types.AX{},
		list,
		scaffoldlist.Options{
			AddtlFlags: flags,
			DefaultColumns: []string{
				// implies embedded namespace
				"ID",
				"Name",
				"Description",

				"Module",
				"Params",
				"Args",
				"Tags",
			},
		})
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.String("id", "", "Fetch extractor by id")
	return addtlFlags
}

func list(fs *pflag.FlagSet) ([]types.AX, error) {
	if id, err := fs.GetString("id"); err != nil {
		uniques.ErrGetFlag("extractors list", err)
	} else if id != "" {
		clilog.Writer.Infof("Fetching ax with id \"%v\"", id)
		d, err := connection.Client.GetExtraction(id)
		return []types.AX{d}, err
	}

	lr, err := connection.Client.ListExtractions(nil)
	return lr.Results, err

}

//#endregion list
