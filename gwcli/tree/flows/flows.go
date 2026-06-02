// Package flows introduces actions for managing flows.
// Due to the visual nature of flows, only a subset of the functionality of the GUI is implemented.
package flows

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/validate"
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
			listFlows(),
			importCreate(),
			download(),
			delete(),
			cancel(),
			backfillToggle(),
			clearResults(),
			parse(),
		},
	)
}

//#region list

func listFlows() action.Pair {
	return scaffoldlist.NewListAction("list flows", "Lists information about flows you can access.",
		types.Flow{},
		func(fs *pflag.FlagSet) ([]types.Flow, error) {
			baseList, err := connection.Client.ListFlows(nil)
			if err != nil {
				return nil, err
			}
			return baseList.Results, nil
		},
		nil,
		scaffoldlist.Options{
			DefaultColumns: []string{
				"CommonFields.ID",
				"CommonFields.Name",
				"CommonFields.Description",
				"AutomationCommonFields.Schedule",
				"AutomationCommonFields.Disabled"},
		},
	)
}

//#endregion list

var validGIDs map[int32]string // cached each SetArg so we don't hit the backend on every key

// importCreate is the create function for flows, but the flow itself is created from JSON slurped from a file
func importCreate() action.Pair {
	return scaffoldcreate.NewCreateAction("flow",
		map[string]scaffoldcreate.Field{
			"name":      scaffoldcreate.FieldName("flow"),
			"desc":      scaffoldcreate.FieldDescription("flow"),
			"frequency": scaffoldcreate.FieldFrequency(),
			"path":      scaffoldcreate.FieldPath("file containing a flow in JSON form"),
			"groups": {
				Required: false,
				Title:    "Groups",
				Flag:     scaffoldcreate.FlagConfig{Name: "groups", Usage: "comma-separated list of group IDs this flow is accessible to", Shorthand: 'g'},
				Provider: &scaffoldcreate.TextProvider{
					CustomInit: func() textinput.Model {
						ti := stylesheet.NewTI("", true)
						ti.Validate = func(s string) error { // returns on first error
							for strGID := range strings.SplitSeq(s, ",") {
								if strGID == "" {
									continue
								}
								// check for numeric only
								if err := validate.Numeric(strGID); err != nil {
									return fmt.Errorf("Group ID: %w", err)
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
					CustomSetArgs: func(ti textinput.Model) textinput.Model {
						// refresh cached group IDs at each invocation
						gm, err := connection.Client.GetGroupMap()
						if err != nil {
							clilog.Writer.Warnf("failed to cache group IDs: %v", err)
						}
						validGIDs = gm
						return ti
					},
				},
				Order: 40,
			},
		},
		func(cfg map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			// slurp the json file
			var json string
			if b, err := os.ReadFile(cfg["path"].Provider.Get()); err != nil {
				return 0, err.Error(), nil // this is probably a file permission or exist error so return as invalid
			} else {
				json = strings.TrimSpace(string(b))
			}

			// coerce groups
			var groups []int32
			for _, s := range strings.Split(cfg["groups"].Provider.Get(), ",") {
				group, err := strconv.ParseInt(s, 10, 32)
				if err != nil {
					clilog.Writer.Warnf("failed to parse %v as int32 for groupID: %v", s, err)
					continue
				}
				groups = append(groups, int32(group))
			}

			spec := types.Flow{
				CommonFields: types.CommonFields{
					Name:        cfg["name"].Provider.Get(),
					Description: cfg["desc"].Provider.Get(),
					Readers:     types.ACL{GIDs: groups},
				},
				AutomationCommonFields: types.AutomationCommonFields{
					Schedule: cfg["frequency"].Provider.Get(),
				},
				Flow: json,
			}
			var result types.Flow
			result, err = connection.Client.CreateFlow(spec)
			id = result.ID
			return
		},
		scaffoldcreate.Options{CommonOptions: scaffold.CommonOptions{Use: "import"}})
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
				clilog.GetFlag(err)
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
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					ft.Output.Register(fs)
					return fs
				},
			},

			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("flow ID/GUID"), nil
				}
				return "", nil
			}})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("flow", "flows",
		func(dryrun bool, id string) error {
			if dryrun {
				_, err := connection.Client.GetFlow(id)
				return err
			}
			return connection.Client.DeleteFlow(id)
		},
		func() ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListFlows(nil)
			if err != nil {
				return nil, err
			}
			var items = make([]multiselectlist.SelectableItem[string], len(lr.Results))
			for i, f := range lr.Results {
				items[i] = &listitem.Generic{
					Selected_:  false,
					ID_:        f.ID,
					Name:       f.Name,
					SecondLine: f.Description,
				}
			}

			return items, nil
		}, scaffolddelete.Options{})
}

func listFlowItems() ([]multiselectlist.SelectableItem[string], error) {
	baseList, err := connection.Client.ListFlows(nil)
	if err != nil {
		return nil, err
	}

	itms := make([]multiselectlist.SelectableItem[string], len(baseList.Results))
	for i, f := range baseList.Results {
		itms[i] = &listitem.Generic{
			ID_:          f.ID,
			Name:         f.Name,
			SecondLine:   fmt.Sprintf("[%s] %s", f.Schedule, f.Description),
			ShowDisabled: true,
			Enabled:      !f.Disabled,
		}
	}
	return itms, nil
}

func getBackfillFlags(fs *pflag.FlagSet) (enable, disable bool, err error) {
	enable, err = fs.GetBool("enable")
	if err != nil {
		clilog.GetFlag(err)
		return
	}
	disable, err = fs.GetBool("disable")
	if err != nil {
		clilog.GetFlag(err)
		return
	}
	if enable && disable {
		return false, false, ft.ErrMutuallyExclusive("enable", "disable")
	}
	return
}

func cancel() action.Pair {
	return scaffoldselect.NewSelectAction("cancel running flows",
		"Cancel one or several currently-executing flows.",
		"flow",
		func(_ *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			// ! this does not filter down to running-only
			// we don't appear to currently have that capability via the client library
			return listFlowItems()
		},
		func(id string, _ *pflag.FlagSet) (success string, err error) {
			if err := connection.Client.CancelFlow(id); err != nil {
				return "", err
			}
			return fmt.Sprintf("successfully cancelled flow %s", id), nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{Use: "cancel"},
		})
}

func backfillToggle() action.Pair {
	return scaffoldselect.NewSelectAction("toggle flow backfill",
		"Toggle backfill for one or several flows.\n"+
			"Backfill causes the automation to run for missed time periods.\n"+
			"Use --enable or --disable to set explicitly.",
		"flow",
		func(fs *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			enable, disable, err := getBackfillFlags(fs)
			if err != nil {
				return nil, err
			}

			baseList, err := connection.Client.ListFlows(nil)
			if err != nil {
				return nil, err
			}
			itms := make([]multiselectlist.SelectableItem[string], 0, len(baseList.Results))
			for _, f := range baseList.Results {
				if enable && f.BackfillEnabled {
					continue
				} else if disable && !f.BackfillEnabled {
					continue
				}
				itms = append(itms, &listitem.Generic{
					ID_:          f.ID,
					Name:         f.Name,
					SecondLine:   fmt.Sprintf("[%s] %s", f.Schedule, f.Description),
					ShowDisabled: true,
					Enabled:      !f.Disabled,
				})
			}
			return itms, nil
		},
		func(id string, fs *pflag.FlagSet) (success string, err error) {
			enable, disable, err := getBackfillFlags(fs)
			if err != nil {
				return "", err
			}

			flow, err := connection.Client.GetFlow(id)
			if err != nil {
				return "", err
			}
			flow.BackfillEnabled = !flow.BackfillEnabled
			if enable {
				flow.BackfillEnabled = true
			} else if disable {
				flow.BackfillEnabled = false
			}

			if err := connection.Client.UpdateFlow(flow); err != nil {
				return "", err
			}
			state := "enabled"
			if !flow.BackfillEnabled {
				state = "disabled"
			}
			return fmt.Sprintf("flow '%s' backfill %s", id, state), nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "toggle-backfill",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("enable", false, "enable backfill")
					fs.Bool("disable", false, "disable backfill")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				_, _, err = getBackfillFlags(fs)
				return "", err
			},
		})
}

func clearResults() action.Pair {
	return scaffoldselect.NewSelectAction("clear results for flows",
		"Clear the execution results (including errors and state) for one or several flows.",
		"flow",
		func(_ *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			return listFlowItems()
		},
		func(id string, _ *pflag.FlagSet) (success string, err error) {
			if err := connection.Client.ClearFlowResults(id); err != nil {
				return "", err
			}
			return fmt.Sprintf("successfully cleared results for flow %s", id), nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{Use: "clear"},
		})
}

// content contained in the file pointed to by --path, if applicable.
var parseFileContent string

// tests a given flow string
func parse() action.Pair {
	return scaffold.NewBasicAction("parse",
		"check the validity of a given flow",
		"Parses a flow string to check it for errors and malformations",
		func(fs *pflag.FlagSet) (output string, addtlCmds tea.Cmd) {
			var flowStr string
			if parseFileContent != "" {
				flowStr = parseFileContent
			} else {
				// slurp from arguments
				flowStr = strings.Join(fs.Args(), " ") // if they were split on spaces, ensure those spaces remain
			}
			res, err := connection.Client.ParseFlow(flowStr)
			if err != nil {
				return err.Error(), nil
			}
			if !res.OK {
				var sb strings.Builder
				for i, npf := range res.Failures {
					fmt.Fprintf(&sb, "Node %d:\n", i)
					for i, err := range npf.Errors {
						fmt.Fprintf(&sb, "\t[%d]: %s\n", i, err)
					}
				}
				return sb.String(), nil
			}
			return "successfully parsed flow", nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					ft.Path.Register(fs, "", "file containing the flow to parse")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				parseFileContent = "" // ensure it is clear before each action
				if pth, err := fs.GetString(ft.Path.Name()); err != nil {
					clilog.GetFlag(err)
				} else if pth = strings.TrimSpace(pth); pth != "" {
					b, err := os.ReadFile(pth)
					if err != nil {
						return err.Error(), nil // probably user error or an issue with the filesystem; return as invalid
					}
					parseFileContent = string(b)
				}
				return "", nil
			},
		})
}
