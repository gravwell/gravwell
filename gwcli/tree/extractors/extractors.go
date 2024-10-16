/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package extractors

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/tree/extractors/create"
	"github.com/gravwell/gravwell/v4/gwcli/tree/extractors/delete"
	"github.com/gravwell/gravwell/v4/gwcli/tree/extractors/list"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
)

const (
	use   string = "extractors"
	short string = "manage your tag autoextractors"
	long  string = "Autoextractors describe how to extract fields from tagged, unstructured data."
)

var aliases []string = []string{"extractor", "ex", "ax", "autoextractor", "autoextractors"}

func NewExtractorsNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{},
		[]action.Pair{
			list.NewExtractorsListAction(),
			create.NewExtractorsCreateAction(),
			delete.NewExtractorDeleteAction()})
}
