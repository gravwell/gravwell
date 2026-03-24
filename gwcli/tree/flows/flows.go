// Package flows introduces actions for managing flows.
// Due to the visual nature of flows, only a subset of the functionality of the GUI is implemented.
package flows

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	return treeutils.GenerateNav("flows",
		"manage no-code automations",
		"Flows provide a no-code method for developing advanced automations in Gravwell."+
			" Flows are automations, meaning they are normally executed on a user-specified schedule by the search agent.",
		[]string{"flow"},
		nil,
		[]action.Pair{
			list(),
			importCreate(),
			download(),
		},
	)
}

//#region list

func list() action.Pair {
	return scaffoldlist.NewListAction("list flows", "Lists information about flows you can access.",
		types.Flow{},
		func(fs *pflag.FlagSet) ([]types.Flow, error) {
			baseList, err := connection.Client.ListFlows(nil)
			if err != nil {
				return nil, err
			}
			return baseList.Results, nil
		},
		scaffoldlist.Options{
			DefaultColumns: []string{"Name", "Description", "ID", "GUID", "Groups", "Global", "Labels", "Owner", "Schedule", "Disabled"},
		},
	)
}

//#endregion list

var validGIDs map[int32]string // cached each SetArg so we don't hit the backend on every key

// importCreate is the create function for flows, but the flow itself is created from JSON slurped from a file
func importCreate() action.Pair {
	return scaffoldcreate.NewCreateAction("flow",
		scaffoldcreate.Config{
			"name":      scaffoldcreate.FieldName("flow"),
			"desc":      scaffoldcreate.FieldDescription("flow"),
			"frequency": scaffoldcreate.FieldFrequency(),
			"path":      scaffoldcreate.FieldPath("file containing a flow in JSON form"),
			"groups": scaffoldcreate.Field{
				Required:      false,
				Title:         "Groups",
				Usage:         "comma-separated list of group IDs this flow is accessible to",
				Type:          scaffoldcreate.Text,
				FlagName:      "groups",
				FlagShorthand: 'g',
				Order:         40,
				CustomTIFuncInit: func() textinput.Model {
					ti := stylesheet.NewTI("", true)
					ti.Validate = func(s string) error { // returns on first error
						for strGID := range strings.SplitSeq(s, ",") {
							// check for numeric only
							for _, r := range strGID {
								if r >= 48 && r <= 57 { // 0-9 in ASCII
									continue
								}
								return errors.New("group IDs may only contain numbers")
							}
							// check that the group exists
							gid, err := strconv.ParseInt(strGID, 10, 32)
							if err != nil {
								clilog.Writer.Infof("failed to parse gid %v as int32: %v", strGID, err)
								continue
							}
							if _, found := validGIDs[int32(gid)]; !found {
								return fmt.Errorf("%v is not a known group ID", gid)
							}
						}
						return nil
					}
					ti.Placeholder = "1,2,5,3,..."
					return ti
				},
				CustomTIFuncSetArg: func(m *textinput.Model) textinput.Model {
					// hijack SetArg to refresh cached group IDs
					gm, err := connection.Client.GetGroupMap()
					if err != nil {
						clilog.Writer.Warnf("failed to cache group IDs: %v", err)
					}
					validGIDs = gm
					return *m
				},
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

			spec := types.Flow{
				CommonFields: types.CommonFields{
					Name:        fieldValues["name"],
					Description: fieldValues["desc"],
					Readers:     types.ACL{GIDs: groups},
				},
				AutomationCommonFields: types.AutomationCommonFields{
					Schedule: fieldValues["frequency"],
				},
				Flow: json,
			}
			var result types.Flow
			result, err = connection.Client.CreateFlow(spec)
			id = result.ID
			return
		},
		scaffoldcreate.Options{Use: "import"})
}

func download() action.Pair {
	return scaffold.NewBasicAction("download", "download the JSON representation of a flow",
		"Download a flow as JSON so it can be re-imported later. Flows can be specified by ID or GUID.\n"+
			"Prints to STDOUT unless -o is specified.",
		func(fs *pflag.FlagSet) (output string, addtlCmds tea.Cmd) {
			flow, err := connection.Client.GetFlow(fs.Arg(0))
			if err != nil {
				return err.Error(), nil
			}
			// check for output
			if outPath, err := fs.GetString(ft.Output.Name()); err != nil {
				clilog.LogFlagFailedGet(ft.Output.Name(), err)
			} else if outPath != "" {
				out, err := os.Create(outPath)
				if err != nil {
					clilog.Writer.Warnf("failed to open %v for writing: %v", outPath, err)
					return
				}
				defer out.Close()
				n, err := out.WriteString(flow.Flow)
				if err != nil {
					return err.Error(), nil
				}
				return phrases.SuccessfullyWroteToFile(n, outPath), nil
			}
			// spit to terminal
			return flow.Flow, nil
		},
		scaffold.BasicOptions{
			AddtlFlagFunc: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				ft.Output.Register(&fs)
				return fs
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("flow ID/GUID"), nil
				}
				return "", nil
			}})
}
