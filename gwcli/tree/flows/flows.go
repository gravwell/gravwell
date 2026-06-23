// Package flows introduces actions for managing flows.
// Due to the visual nature of flows, only a subset of the functionality of the GUI is implemented.
package flows

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/dustin/go-humanize/english"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/internal/state"
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
			listActions(),
			importCreate(),
			download(),
			delete(),
			cancel(),
			backfillToggle(),
			clear(),
			parse(),
			create(),
		},
	)
}

func listActions() action.Pair {
	return scaffoldlist.NewListAction("list flows", "Lists information about flows you can access.",
		types.Flow{},
		func(fs *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.Flow, error) {
			baseList, err := connection.Client.ListFlows(params.QueryOpts)
			return baseList.Results, err
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

// importCreate is the create function for flows, but the flow itself is created from JSON slurped from a file
func importCreate() action.Pair {
	var validGIDs map[int32]string // cached each SetArg so we don't hit the backend on every key
	return scaffoldcreate.NewCreateAction("flow",
		map[string]scaffoldcreate.Field{
			"name":      scaffoldcreate.FieldName("flow"),
			"desc":      scaffoldcreate.FieldDescription("flow"),
			"frequency": scaffoldcreate.FieldFrequency(),
			"path":      scaffoldcreate.FieldPath("file containing a flow in JSON form", true),
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
	return scaffolddelete.NewDeleteAction("flow",
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
			return listitem.WrapFlows(lr.Results), nil
		}, scaffolddelete.Options{})
}

func listFlowItems() ([]multiselectlist.SelectableItem[string], error) {
	lr, err := connection.Client.ListFlows(nil)
	if err != nil {
		return nil, err
	}
	return listitem.WrapFlows(lr.Results), nil
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
		func(IDs []string, _ *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				if err := connection.Client.CancelFlow(ID); err != nil {
					results[i] = scaffold.Result{Success: false, Output: fmt.Sprintf("failed to cancel flow %s: %v", ID, err)}
				} else {
					results[i] = scaffold.Result{Success: true, Output: fmt.Sprintf("successfully cancelled flow %s", ID)}
				}
			}
			return results, nil
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
			itms := make([]types.Flow, 0, len(baseList.Results))
			for _, f := range baseList.Results {
				if enable && f.BackfillEnabled {
					continue
				} else if disable && !f.BackfillEnabled {
					continue
				}
				itms = append(itms, f)
			}
			return listitem.WrapFlows(slices.Clip(itms)), nil
		},
		func(IDs []string, fs *pflag.FlagSet) (results []scaffold.Result, _ error) {
			enable, disable, err := getBackfillFlags(fs)
			if err != nil {
				return nil, err
			}

			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				flow, err := connection.Client.GetFlow(ID)
				if err != nil {
					results[i] = scaffold.Result{Success: false, Output: fmt.Sprintf("failed to get flow %s: %v", ID, err)}
					continue
				}
				flow.BackfillEnabled = !flow.BackfillEnabled
				if enable {
					flow.BackfillEnabled = true
				} else if disable {
					flow.BackfillEnabled = false
				}

				if err := connection.Client.UpdateFlow(flow); err != nil {
					results[i] = scaffold.Result{Success: false, Output: fmt.Sprintf("failed to update backfill for flow %s: %v", ID, err)}
					continue
				}
				state := "enabled"
				if !flow.BackfillEnabled {
					state = "disabled"
				}
				results[i] = scaffold.Result{Success: true, Output: fmt.Sprintf("flow '%s' backfill %s", ID, state)}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "backfill",
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

func clear() action.Pair {
	return scaffoldselect.NewSelectAction("clear results for flows",
		"Clear the execution results (including errors and state) for one or several flows.",
		"flow",
		func(_ *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			return listFlowItems()
		},
		func(IDs []string, _ *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				if err := connection.Client.ClearFlowResults(ID); err != nil {
					results[i] = scaffold.Result{Success: false, Output: fmt.Sprintf("failed to clear results for flow %s: %v", ID, err)}
				} else {
					results[i] = scaffold.Result{Success: true, Output: fmt.Sprintf("successfully cleared results for flow %s", ID)}
				}
			}
			return results, nil
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
			res, err := connection.Client.ParseFlow(parseFileContent)
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
				Usage: "parse " + ft.MutuallyExclusive([]string{"<flow string>", "--path=path/to/file"}),
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					ft.Path.Register(fs, "", "file containing the flow to parse.\n"+
						"Mutually exclusive with --stdin and bare arguments")
					fs.Bool("stdin", false, ft.NonInteractiveOnly()+" read the flow string from stdin.\n"+
						"Mutually exclusive with --path")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				parseFileContent = "" // ensure it is clear before each action

				bare := []string{}
				for _, arg := range fs.Args() {
					arg = strings.TrimSpace(arg)
					if arg != "" {
						bare = append(bare, arg)
					}
				}
				pth, err := fs.GetString(ft.Path.Name())
				clilog.GetFlag(err)
				stdin, err := fs.GetBool("stdin")
				clilog.GetFlag(err)
				if state.Interactive() && stdin {
					return phrases.ErrFlagNoInteractiveOnly("--stdin").Error(), nil
				}
				if (pth != "" && stdin) || (pth != "" && len(bare) > 0) || (stdin && len(bare) > 0) { // check for MX
					return english.OxfordWordSeries([]string{"--path", "--stdin", "bare arguments"}, "and") + " are mutually exclusive", nil
				}

				// figure out where we are reading from
				if pth = strings.TrimSpace(pth); pth != "" {
					b, err := os.ReadFile(pth)
					if err != nil {
						return err.Error(), nil // probably user error or an issue with the filesystem; return as invalid
					}
					parseFileContent = string(b)
				} else if stdin {
					b, err := io.ReadAll(os.Stdin)
					if err != nil {
						return err.Error(), nil
					}
					parseFileContent = string(b)
				} else if len(bare) > 0 {
					parseFileContent = strings.Join(bare, " ") // if they were split on spaces, ensure those spaces remain
				} else { // nothing was set, fail out
					return "one of --path, --stdin, or bare argument is required", nil
				}
				return "", nil
			},
		})
}

func create() action.Pair {
	return scaffoldcreate.NewCreateAction("flow",
		map[string]scaffoldcreate.Field{
			"name":     scaffoldcreate.FieldName("flow"),
			"desc":     scaffoldcreate.FieldDescription("flow"),
			"path":     scaffoldcreate.FieldPath("flow specification", true),
			"labels":   scaffoldcreate.FieldLabels(),
			"schedule": scaffoldcreate.FieldFrequency(),
			"enabled": {
				Title: "Enabled?", Required: false,
				Flag:     scaffoldcreate.FlagConfig{},
				Order:    30,
				Provider: &scaffoldcreate.BoolProvider{},
			},
			"backfill": {
				Title: "Backfill?", Required: false,
				Flag:     scaffoldcreate.FlagConfig{},
				Order:    20,
				Provider: &scaffoldcreate.BoolProvider{},
			},
		},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			pth := fields["path"].Provider.Get()
			flowContent, err := os.ReadFile(pth)
			if err != nil {
				return 0, "", err
			}

			enabled, _ := strconv.ParseBool(fields["enabled"].Provider.Get())
			backfill, _ := strconv.ParseBool(fields["backfill"].Provider.Get())

			newFlow, err := connection.Client.CreateFlow(types.Flow{
				CommonFields: types.CommonFields{
					Name:        fields["name"].Provider.Get(),
					Description: fields["desc"].Provider.Get(),
					Labels:      scaffoldcreate.GetLabelsFromField(fields["labels"]),
				},
				AutomationCommonFields: types.AutomationCommonFields{
					Schedule:        fields["schedule"].Provider.Get(),
					Disabled:        !enabled,
					BackfillEnabled: backfill,
				},
				Flow: string(flowContent),
			})
			if err != nil {
				return 0, "", err
			}
			return newFlow.ID, "", nil
		},
		scaffoldcreate.Options{})

}
