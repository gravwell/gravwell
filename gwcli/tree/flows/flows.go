package flows

import (
	"os"
	"strconv"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	return treeutils.GenerateNav("flows",
		"", "", // TODO
		[]string{"flow"},
		nil,
		[]action.Pair{
			list(),
			importCreate(),
		},
	)
}

func list() action.Pair {
	return scaffoldlist.NewListAction("list flows", "", // TODO
		types.ScheduledSearch{},
		func(fs *pflag.FlagSet) ([]types.ScheduledSearch, error) {
			return connection.Client.GetFlowList()
		},
		scaffoldlist.Options{},
	)
}

// importCreate is the create function for flows, but the flow itself is created from JSON slurped from a file
func importCreate() action.Pair {
	return scaffoldcreate.NewCreateAction("flow",
		scaffoldcreate.Config{
			"name": scaffoldcreate.FieldName("flow"),
			"desc": scaffoldcreate.FieldDescription("flow"),
			"frequency": scaffoldcreate.Field{
				Required:      true,
				Usage:         ft.Frequency.Usage(),
				Type:          scaffoldcreate.Text,
				FlagName:      ft.Frequency.Name(),
				FlagShorthand: rune(ft.Frequency.Shorthand()[0]),
			},
			"path": scaffoldcreate.FieldPath("file containing a flow in JSON form"),
			"groups": scaffoldcreate.Field{
				Required:      false,
				Title:         "Groups",
				Usage:         "comma-separated list of group IDs this flow is accessible to",
				Type:          scaffoldcreate.File,
				FlagName:      "groups",
				FlagShorthand: 'g',
				Order:         40,
			},
		},
		func(cfg scaffoldcreate.Config, fieldValues map[string]string, fs *pflag.FlagSet) (id any, invalid string, err error) {
			// slurp the json file
			var json string
			if b, err := os.ReadFile(fieldValues["path"]); err != nil {
				return 0, err.Error(), nil // this is probably a file permission or exist error so return as invalid
			} else {
				json = strings.TrimSpace(string(b))
			}

			// coerce groups
			var groups []int32
			for _, s := range strings.Split(fieldValues["groups"], ",") {
				group, err := strconv.ParseInt(s, 10, 32)
				if err != nil {
					clilog.Writer.Warnf("failed to parse %v as int32 for groupID: %v", s, err)
					continue
				}
				groups = append(groups, int32(group))
			}

			id, err = connection.Client.CreateFlow(fieldValues["name"], fieldValues["desc"], fieldValues["frequency"], json, groups)
			return id, "", err
		},
		scaffoldcreate.Options{Use: "import"})
}
