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
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/annotations"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// NewNav returns a nav based around manipulating autoextractors.
func NewNav() *cobra.Command {
	const (
		use   string = "extractors"
		short string = "manage your tag autoextractors"
		long  string = "Autoextractors describe how to extract fields from tagged, unstructured data."
	)

	var aliases = []string{"extractor", "ex", "ax", "autoextractor", "autoextractors", "axs"}

	return treeutils.GenerateNav(use, short, long,
		[]*cobra.Command{},
		[]action.Pair{
			list(),
			create(),
			delete(),
			modules(),
			edit(),
			importUpload(),
			find(),
			clear(),
		}, treeutils.NodeOptions{CommandAliases: aliases})
}

// ensure consistency between edit and create
const (
	fieldUsageParams = "The extraction definition. It must be single (raw) or double quoted (subject to string escape rules).\n" +
		"Usage and syntax is dependant on module.\n" +
		"See https://docs.gravwell.io/configuration/autoextractors.html for more info"
	fieldUsageArgs = "Module-specific arguments used to change the behavior of the extraction module"
)

var (
	fieldUsageModule = "Extraction module to use. Call " + stylesheet.Path(true, "~", "extractors", "modules") + " to list available options."
	fieldUsageTags   = "Tags this ax will extract from. Call " + stylesheet.Path(true, "~", "tags") + " to list available options. " +
		"There can only be one extractor per tag."
)

// #region list

func list() action.Pair {
	return scaffoldlist.NewListAction(
		"list extractors",
		"List autoextractions available to you",
		types.AX{},
		func(fs *pflag.FlagSet, param scaffoldlist.DataParameters) ([]types.AX, error) {
			id, err := fs.GetString("id")
			clilog.GetFlag(err)
			if id != "" {
				clilog.Writer.Info("Fetching ax by ID", log.KV("ID", id))
				d, err := connection.Client.GetExtraction(id) // TODO this needs an EX equivalent, no?
				return []types.AX{d}, err
			}

			lr, err := connection.Client.ListExtractions(param.QueryOpts)
			return lr.Results, err

		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: flags,
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ExtractorRead},
					XPermissions: []types.Capability{types.ExtractorRead},
				},
			},
			DefaultColumns: []string{
				"CommonFields.ID",
				"CommonFields.Name",
				"CommonFields.Description",
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
		map[string]scaffoldcreate.Field{
			"name": scaffoldcreate.FieldName("extractor"),
			"desc": scaffoldcreate.FieldDescription("extractor"),
			"module": {
				Required: true,
				Title:    "Module",
				Flag:     scaffoldcreate.FlagConfig{Name: "module", Usage: fieldUsageModule},
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
			"tags": {
				Required: true,
				Title:    "Tags",
				Flag:     scaffoldcreate.FlagConfig{Name: "tags", Usage: fieldUsageTags},
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
			"params": {
				Required: true,
				Title:    "Parameters",
				Flag:     scaffoldcreate.FlagConfig{Name: "module-parameters", Usage: fieldUsageParams},
				Provider: &scaffoldcreate.TextProvider{},
				Order:    60,
			},
			"args": {
				Required:     false,
				Title:        "Arguments",
				Flag:         scaffoldcreate.FlagConfig{Name: "module-arguments", Usage: fieldUsageArgs},
				Provider:     &scaffoldcreate.TextProvider{},
				DefaultValue: "",
				Order:        50,
			},
			"labels": fLabels,
		},
		func(cfg map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (any, string, error) {
			// no need to nil check; Required boolean enforces that for us

			// map fields back into the underlying type
			axd := types.AX{
				CommonFields: types.CommonFields{
					Name:        cfg["name"].Provider.Get(),
					Description: cfg["desc"].Provider.Get(),
					Labels:      scaffoldcreate.GetLabelsFromField(cfg["labels"]),
				},
				Module: cfg["module"].Provider.Get(),
				Tags:   strings.Split(strings.ReplaceAll(cfg["tags"].Provider.Get(), " ", ""), ","),
				Params: cfg["params"].Provider.Get(),
				Args:   cfg["args"].Provider.Get(),
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
				Example: "create --" + ft.Name.Name() + "=testcsv " +
					"--" + ft.Description.Name() + "=\"CSV auto-extraction for the super ugly CSV data\" " +
					"--tags=csv" +
					"--module csv " +
					"--module-parameters " + "\"ts, name, id, guid, src, srcport, dst, dstport, data, country, city, hash\"",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					ft.Dryrun.Register(fs)
					return fs
				},
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ExtractorWrite},
					XPermissions: []types.Capability{types.ExtractorWrite},
				},
			},
		})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("extractor",
		func(dryrun bool, id string, _ *pflag.FlagSet) error {
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
					sb.WriteString("\n")
					sb.WriteString(wr.Err.Error())
				}
				clilog.Writer.Warn(sb.String())
				return errors.New(sb.String())
			}
			return nil
		},
		func(params scaffolddelete.DataParameters) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListExtractions(params.QueryOpts)
			if err != nil {
				return nil, err
			}

			return listitem.WrapAssets(lr.Results), nil
		}, scaffolddelete.Options{
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ExtractorRead, types.ExtractorWrite},
					XPermissions: []types.Capability{types.ExtractorWrite},
				},
			},
			QueryOptionsFlags: scaffold.QOInclude{Everything: true},
		},
	)
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
		"name": scaffoldedit.FieldName("extractor"),
		"desc": scaffoldedit.FieldDescription("extractor"),
		"module": &scaffoldedit.Field{
			Required: true,
			Title:    "Module",
			Usage:    fieldUsageModule,
			FlagName: "module",
			Order:    80,
		},
		"tags": &scaffoldedit.Field{
			Required: true,
			Title:    "Tags",
			Usage:    fieldUsageTags,
			FlagName: "tags",
			Order:    70,
			CustomTIFuncInit: func() textinput.Model {
				ti := stylesheet.NewTI("", false)
				ti.Placeholder = "tag1,tag2,tag3"
				return ti
			},
		},
		"params": &scaffoldedit.Field{
			Required: false,
			Title:    "Parameters",
			FlagName: "module-parameters",
			Usage:    fieldUsageParams,
			Order:    60,
		},
		"args": &scaffoldedit.Field{
			Required: false,
			Title:    "Arguments",
			FlagName: "module-arguments",
			Usage:    fieldUsageArgs,
			Order:    50,
		},
		"labels": fLabels,
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
				case "name":
					return item.Name, nil
				case "desc":
					return item.Description, nil
				case "module":
					return item.Module, nil
				case "tags":
					return strings.Join(item.Tags, ","), nil
				case "params":
					return item.Params, nil
				case "args":
					return item.Args, nil
				case "labels":
					return strings.Join(item.Labels, ","), nil
				}
				return "", fmt.Errorf("unknown field key: %v", fieldKey)
			},
			SetFieldSub: func(item *types.AX, fieldKey, val string) (invalid string, err error) {
				switch fieldKey {
				case "name":
					item.Name = val
				case "desc":
					item.Description = val
				case "module":
					item.Module = val
				case "tags":
					item.Tags = strings.Split(val, ",")
				case "params":
					item.Params = val
				case "args":
					item.Args = val
				case "labels":
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
				if _, err := connection.Client.UpdateExtraction(data.ID, data.ToPatch()); err != nil {
					return "", err
				}
				return data.Name, nil
			},
		},
		scaffoldedit.Options{CommonOptions: scaffold.CommonOptions{Requirements: annotations.Requirements{
			IPermissions: []types.Capability{types.ExtractorRead, types.ExtractorWrite},
			XPermissions: []types.Capability{types.ExtractorRead, types.ExtractorWrite},
		}}},
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
					sb.WriteString(stylesheet.Cur.ErrorText.Render(fmt.Sprintf("Warning: %v: %v", warn.Name, warn.Err)))
					sb.WriteString("\n")
				}

				clilog.Writer.Warn("extractor update caused warnings", params...)
			}
			sb.WriteString(phrases.SuccessfullyLoadedFile(fs.Arg(0)))
			return sb.String(), nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Usage: "import " + ft.Mandatory("path/to/file.toml"),
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ExtractorWrite},
					XPermissions: []types.Capability{types.ExtractorWrite},
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("file path"), nil
				}
				return "", nil
			},
		})
}

func find() action.Pair {
	return scaffoldselect.NewSelectAction("find a tag's extractor", "Display information about the extractor associated to a selected tag",
		"tag",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			tags, err := connection.Client.GetTags()
			if err != nil {
				return nil, err
			}
			data := make([]types.AX, 0, len(tags))
			for _, tag := range tags {
				ax, err := connection.Client.FindExtraction(tag)
				if phrases.IsNotFoundErr(err) { // no associated ax
					continue
				} else if err != nil {
					return nil, err
				}
				data = append(data, ax)
			}
			return listitem.WrapAssets(slices.Clip(data)), nil
		},
		func(tags []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(tags))
			for i, tag := range tags {
				ax, err := connection.Client.FindExtraction(tag)
				if phrases.IsNotFoundErr(err) {
					results[i].Output = "tag '" + tag + "' does not have an extractor"
					results[i].Success = true
				} else if err != nil {
					results[i].Output = "failed to get extractor for " + tag
				} else {
					results[i].Output = fmt.Sprintf("tag '%v' uses extractor '%v'\n"+
						"ID: %v\n"+
						"Module: %v\n"+
						"Args: %v\n"+
						"Other tags: %v",
						tag, ax.Name, ax.ID, ax.Module, ax.Args, slices.DeleteFunc(ax.Tags, func(s string) bool { return s == tag }))
					results[i].Success = true
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "find",
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ExtractorRead},
					XPermissions: []types.Capability{types.ExtractorRead},
				},
			},
		})
}

func clear() action.Pair {
	return scaffoldselect.NewSelectAction("clear a tag's extractor", "Unassign and delete whatever extractor is on the given tag(s).", "ax",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListExtractions(&types.QueryOptions{AdminMode: connection.AdminMode()})
			if err != nil {
				return nil, err
			}
			// make a list of tags that have extractors
			tags := map[string]types.AX{}
			for _, ax := range lr.Results {
				for _, t := range ax.Tags {
					tags[t] = ax
				}
			}
			data := make([]multiselectlist.SelectableItem[string], len(tags))
			var i int
			for tag, ax := range tags {
				data[i] = &listitem.Generic{
					ID_:        tag,
					Name:       tag,
					SecondLine: fmt.Sprintf("AX: %s (ID: %s)", ax.Name, ax.ID),
				}
				i += 1
			}

			return data, nil
		},
		func(tags []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(tags))

			for i, tag := range tags {
				ax, err := connection.Client.FindExtraction(tag)
				if phrases.IsNotFoundErr(err) {
					results[i] = scaffold.Result{Success: true, Output: "tag '" + tag + "' does not have an extractor"}
					continue
				}
				if err != nil {
					results[i] = scaffold.Result{Output: err.Error()}
					continue
				}

				warns, err := connection.Client.DeleteExtraction(ax.ID)
				if err != nil {
					results[i] = scaffold.Result{Output: "failed to update ax: " + err.Error()}
					continue
				} else if len(warns) > 0 {
					clilog.Writer.Warn("updating the AX triggered warnings", log.KV("warnings", warns))
				}
				results[i] = scaffold.Result{Success: true, Output: "removed ax '" + ax.Name + "' from tag '" + tag + "'"}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "clear",
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ExtractorRead, types.ExtractorWrite},
					XPermissions: []types.Capability{types.ExtractorRead, types.ExtractorWrite},
				},
			},
		})
}
