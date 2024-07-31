package extractors

import (
	"gwcli/action"
	"gwcli/tree/extractors/create"
	"gwcli/tree/extractors/delete"
	"gwcli/tree/extractors/list"
	"gwcli/utilities/treeutils"

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
