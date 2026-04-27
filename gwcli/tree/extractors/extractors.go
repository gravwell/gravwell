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
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
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
			list(),
			create(),
			delete(),
			modules(),
			edit(),
			importUpload(),
		})
}

// field map keys used by edit and create for consistent access.
const (
	fieldKeyName     = "name"
	fieldKeyDesc     = "desc"
	fieldKeyModule   = "module"
	fieldKeyTags     = "tags"
	fieldKeyParams   = "params"
	fieldUsageParams = "regex to apply to extract.\n" +
		"There are a few important notes about how an extraction parameter is defined:\n" +
		"1) Each extraction parameter's value must be defined as a string and double or single quoted.\n" +
		`2) Double quoted strings are subject to string escape rules (pay attention when using regex).` + "\n" +
		`ex: “\b” would be the backspace command (character 0x08) not the literal “\b".` + "\n" +
		`3) Single quoted strings are raw and not subjected to string escape rules.` + "\n" +
		`ex: '\b' is literally the backslash character followed by the 'b' character, not a backspace.`
	fieldKeyArgs   = "args"
	fieldUsageArgs = "module-specific arguments used to change the behavior of the extraction module.\n" +
		"NOTE: The regex processor does not support arguments"
	fieldKeyLabels = "labels"
)

// #region list

func list() action.Pair {
	const (
		short string = "list extractors"
		long  string = "list autoextractions available to you and the system"
	)

	return scaffoldlist.NewListAction(
		short,
		long,
		types.AX{},
		func(fs *pflag.FlagSet) ([]types.AX, error) {
			if id, err := fs.GetString("id"); err != nil {
				uniques.ErrGetFlag("extractors list", err)
			} else if id != "" {
				clilog.Writer.Infof("Fetching ax with id \"%v\"", id)
				d, err := connection.Client.GetExtraction(id)
				return []types.AX{d}, err
			}

			lr, err := connection.Client.ListExtractions(nil)
			return lr.Results, err

		},
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{AddtlFlags: flags},
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

func flags() *pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.String("id", "", "Fetch extractor by id")
	return &addtlFlags
}

//#endregion list

func create() action.Pair {
	fLabels := scaffoldcreate.FieldLabels()
	fLabels.Order = 40

	return scaffoldcreate.NewCreateAction("extractor",
		scaffoldcreate.Config{
			fieldKeyName: scaffoldcreate.FieldName("extractor"),
			fieldKeyDesc: scaffoldcreate.FieldDescription("extractor"),
			fieldKeyModule: scaffoldcreate.Field{
				Required: true,
				Title:    "module",
				Flag:     scaffoldcreate.FlagConfig{Name: "module", Usage: "extraction module to use. Call `extractors modules` to list available options.", Shorthand: 'm'},
				Provider: &scaffoldcreate.TextProvider{
					CustomInit: func() textinput.Model {
						ti := stylesheet.NewTI("", false)
						ti.ShowSuggestions = true
						return ti
					},
					CustomSetArgs: func(ti textinput.Model) textinput.Model {
						if engines, err := connection.Client.ExtractionSupportedEngines(); err != nil {
							clilog.Writer.Warnf("failed to gather modules for suggestions: %v", err)
						} else if len(engines) > 0 {
							ti.SetSuggestions(engines)
							ti.Placeholder = engines[0]
						}
						return ti
					},
				},
				DefaultValue: "",
				Order:        80,
			},
			fieldKeyTags: scaffoldcreate.Field{
				Required: true,
				Title:    "tags",
				Flag:     scaffoldcreate.FlagConfig{Name: "tags", Usage: "tags this ax will extract from. There can only be one extractor per tag.", Shorthand: 't'},
				Provider: &scaffoldcreate.TextProvider{
					CustomInit: func() textinput.Model {
						ti := stylesheet.NewTI("", false)
						ti.Placeholder = "tag1,tag2,tag3"
						return ti
					},
					CustomSetArgs: func(ti textinput.Model) textinput.Model {
						if tags, err := connection.Client.GetTags(); err != nil {
							clilog.Writer.Warnf("failed to fetch tags: %v", err)
							ti.ShowSuggestions = false
						} else {
							ti.ShowSuggestions = true
							ti.SetSuggestions(tags)
						}
						return ti
					},
				},
				Order: 70,
			},
			fieldKeyParams: scaffoldcreate.Field{
				Required: true,
				Title:    "Params/regex",
				Flag:     scaffoldcreate.FlagConfig{Name: "params", Usage: fieldUsageParams},
				Provider: &scaffoldcreate.TextProvider{},
				Order:    60,
			},
			fieldKeyArgs: scaffoldcreate.Field{
				Required:     false,
				Title:        "arguments/options",
				Flag:         scaffoldcreate.FlagConfig{Name: "args", Usage: fieldUsageArgs},
				Provider:     &scaffoldcreate.TextProvider{},
				DefaultValue: "",
				Order:        50,
			},
			fieldKeyLabels: fLabels,
		},
		func(cfg scaffoldcreate.Config, fs *pflag.FlagSet) (any, string, error) {
			// no need to nil check; Required boolean enforces that for us

			// map fields back into the underlying type
			axd := types.AX{
				CommonFields: types.CommonFields{
					Name:        cfg[fieldKeyName].Provider.Get(),
					Description: cfg[fieldKeyDesc].Provider.Get(),
					Labels:      strings.Split(strings.ReplaceAll(cfg[fieldKeyLabels].Provider.Get(), " ", ""), ","),
				},
				Module: cfg[fieldKeyModule].Provider.Get(),
				Tags:   strings.Split(strings.ReplaceAll(cfg[fieldKeyTags].Provider.Get(), " ", ""), ","),
				Params: cfg[fieldKeyParams].Provider.Get(),
				Args:   cfg[fieldKeyArgs].Provider.Get(),
			}

			// check for dryrun
			var (
				dr  bool
				err error
			)
			if dr, err = fs.GetBool(ft.Dryrun.Name()); err != nil {
				return 0, "", err
			}

			var (
				id  string
				wrs []types.WarnResp
			)
			if dr {
				wrs, err = connection.Client.TestAddExtraction(axd)
			} else {
				axd, wrs, err = connection.Client.AddExtraction(axd)
				id = axd.ID
			}

			if len(wrs) > 0 {
				var invSB strings.Builder
				for _, wr := range wrs {
					fmt.Fprintf(&invSB, "%v: %v\n", wr.Name, wr.Err)
				}
				return 0, invSB.String(), nil
			}

			return id, "", err
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					ft.Dryrun.Register(fs)
					return fs
				},
			},
		})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("extractor", "extractors",
		func(dryrun bool, id string) error {
			if dryrun {
				_, err := connection.Client.GetExtraction(id)
				return err
			}
			if wrs, err := connection.Client.DeleteExtraction(id); err != nil {
				return err
			} else if wrs != nil {
				var sb strings.Builder
				sb.WriteString("failed to delete ax with warning(s):")
				for _, wr := range wrs {
					sb.WriteString("\n" + wr.Err.Error())
				}
				clilog.Writer.Warn(sb.String())
				return errors.New(sb.String())
			}
			return nil
		},
		func() ([]scaffolddelete.Item[string], error) {
			axl, err := connection.Client.ListExtractions(nil)
			if err != nil {
				return nil, err
			}
			axs := axl.Results
			slices.SortFunc(axs, func(a1, a2 types.AX) int {
				return strings.Compare(a1.Name, a2.Name)
			})
			var items = make([]scaffolddelete.Item[string], len(axs))
			for i, ax := range axs {
				items[i] = scaffolddelete.NewItem[string](ax.Name,
					fmt.Sprintf("module: %v\ntags: %v\n%v",
						stylesheet.Cur.SecondaryText.Render(ax.Module),
						stylesheet.Cur.SecondaryText.Render(strings.Join(ax.Tags, " ")),
						ax.Description),
					ax.ID)
			}

			return items, nil
		})
}

func modules() action.Pair {
	return scaffold.NewBasicAction("modules", "list available modules",
		"Displays a list of autoextractor modules currently on the system."+
			" Auto-extractors are simply definitions that can be applied to tags and describe how to correctly extract fields from the data in a given tag."+
			" The “ax” module then automatically invokes the appropriate functionality of other modules.",
		func(fs *pflag.FlagSet) (output string, addtlCmds tea.Cmd) {
			engines, err := connection.Client.ExtractionSupportedEngines()
			if err != nil {
				return err.Error(), nil
			}
			return strings.Join(engines, ", "), nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Aliases: []string{"engines"},
			},
		})

}

// NOTE(rlandau): edit fields do not currently support SetArgs injections so, unlike create, edit does NOT support dynamic suggestions.
func edit() action.Pair {
	fLabels := scaffoldedit.FieldLabels()
	fLabels.Order = 40
	return scaffoldedit.NewEditAction("extractor", "extractors", scaffoldedit.Config{
		fieldKeyName: scaffoldedit.FieldName("extractor"),
		fieldKeyDesc: scaffoldedit.FieldDescription("extractor"),
		fieldKeyModule: &scaffoldedit.Field{
			Required:      true,
			Title:         "module",
			Usage:         "extraction module to use. Call `extractors modules` to list available options.",
			FlagName:      "module",
			FlagShorthand: 'm',
			Order:         80,
		},
		fieldKeyTags: &scaffoldedit.Field{
			Required:      true,
			Title:         "tags",
			Usage:         "tags this ax will extract from. There can only be one extractor per tag.",
			FlagName:      "tags",
			FlagShorthand: 't',
			Order:         70,
			CustomTIFuncInit: func() textinput.Model {
				ti := stylesheet.NewTI("", false)
				ti.Placeholder = "tag1,tag2,tag3"
				return ti
			},
		},
		fieldKeyParams: &scaffoldedit.Field{
			Required: false,
			Title:    "params/regex",
			Usage:    fieldUsageParams,
			FlagName: "params",
			Order:    60,
		},
		fieldKeyArgs: &scaffoldedit.Field{
			Required: false,
			Title:    "arguments/options",
			Usage:    fieldUsageArgs,
			FlagName: "args",
			Order:    50,
		},
		fieldKeyLabels: fLabels,
	},
		scaffoldedit.SubroutineSet[string, types.AX]{
			SelectSub: func(id string) (item types.AX, err error) {
				return connection.Client.GetExtraction(id)
			},
			FetchSub: func() (items []types.AX, err error) {
				resp, err := connection.Client.ListExtractions(nil)
				if err != nil {
					return nil, err
				}
				return resp.Results, nil
			},
			GetFieldSub: func(item types.AX, fieldKey string) (value string, err error) {
				switch fieldKey {
				case fieldKeyName:
					return item.Name, nil
				case fieldKeyDesc:
					return item.Description, nil
				case fieldKeyModule:
					return item.Module, nil
				case fieldKeyTags:
					return strings.Join(item.Tags, ","), nil
				case fieldKeyParams:
					return item.Params, nil
				case fieldKeyArgs:
					return item.Args, nil
				case fieldKeyLabels:
					return strings.Join(item.Labels, ","), nil
				}
				return "", fmt.Errorf("unknown field key: %v", fieldKey)
			},
			SetFieldSub: func(item *types.AX, fieldKey, val string) (invalid string, err error) {
				switch fieldKey {
				case fieldKeyName:
					item.Name = val
				case fieldKeyDesc:
					item.Description = val
				case fieldKeyModule:
					item.Module = val
				case fieldKeyTags:
					item.Tags = strings.Split(val, ",")
				case fieldKeyParams:
					item.Params = val
				case fieldKeyArgs:
					item.Args = val
				case fieldKeyLabels:
					item.Labels = strings.Split(val, ",")
				default:
					return "", fmt.Errorf("unknown field key: %v", fieldKey)
				}
				return "", nil
			},
			GetTitleSub: func(item types.AX) string {
				return item.Name
			},
			GetDescriptionSub: func(item types.AX) string {
				return item.Description
			},
			UpdateSub: func(data *types.AX) (identifier string, err error) {
				if data == nil {
					clilog.Writer.Error("update subroutine given nil data!")
					return "", errors.New("an error occurred")
				}
				warnings, err := connection.Client.UpdateExtraction(*data)
				if err != nil {
					return "", err
				}
				if len(warnings) > 0 {
					var params = make([]rfc5424.SDParam, len(warnings))
					for i, warn := range warnings {
						params[i] = rfc5424.SDParam{
							Name:  fmt.Sprint("warning", i),
							Value: fmt.Sprint(warn.Name, ": ", warn.Err),
						}
					}

					clilog.Writer.Warn("extractor update caused warnings", params...)
				}
				return data.Name, nil
			},
		},
	)
}

func importUpload() action.Pair {
	return scaffold.NewBasicAction("import", "import extractor from file",
		"Uploads a TOML-formatted file containing one or more autoextractor definitions.\n"+
			"Gravwell will parse these definitions and install or update autoextractors as appropriate.",
		func(fs *pflag.FlagSet) (output string, addtlCmds tea.Cmd) {
			b, err := os.ReadFile(fs.Arg(0))
			if err != nil {
				return err.Error(), nil
			}
			warnings, err := connection.Client.UploadExtraction(b)
			if err != nil {
				return err.Error(), nil
			}
			var sb strings.Builder
			if len(warnings) > 0 {
				var params = make([]rfc5424.SDParam, len(warnings))
				for i, warn := range warnings {
					params[i] = rfc5424.SDParam{
						Name:  warn.Name,
						Value: fmt.Sprint(warn.Err),
					}
					sb.WriteString(stylesheet.Cur.ErrorText.Render(fmt.Sprintf("Warning: %v: %v", warn.Name, warn.Err)) + "\n")
				}

				clilog.Writer.Warn("extractor update caused warnings", params...)
			}
			sb.WriteString(phrases.SuccessfullyLoadedFile(fs.Arg(0)))
			return sb.String(), nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Usage: "import " + ft.Mandatory("path/to/file.toml"),
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("file path"), nil
				}
				return "", nil
			},
		})
}
