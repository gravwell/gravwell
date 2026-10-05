/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package skills provides actions for managing agent skills: listing them,
// and uploading, replacing, downloading and deleting them as markdown files.
package skills

import (
	"errors"
	"os"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/annotations"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const singular = "skill"

// NewNav returns the skills nav.
func NewNav() *cobra.Command {
	return treeutils.GenerateNav("skills", "manage agent skills",
		"Skills are markdown documents an AI agent can load on demand; their name and description are advertised to the agent.\n"+
			"A skill file may open with a front matter block (--- name: ... description: ... ---) naming it; otherwise it is named after the file.",
		nil,
		[]action.Pair{
			list(),
			upload(),
			replace(),
			download(),
			delete(),
		}, treeutils.NodeOptions{CommandAliases: []string{"skill"}})
}

// wrappedSkill flattens a skill for the list view: the body becomes its size.
type wrappedSkill struct {
	types.CommonFields

	BodyBytes int
}

func wrap(skills []types.AgentSkill) []wrappedSkill {
	out := make([]wrappedSkill, len(skills))
	for i, s := range skills {
		out[i] = wrappedSkill{CommonFields: s.CommonFields, BodyBytes: len(s.Body)}
	}
	return out
}

func list() action.Pair {
	return scaffoldlist.NewListAction("list skills", "List the agent skills available to your user.",
		wrappedSkill{},
		func(fs *pflag.FlagSet, params scaffoldlist.DataParameters) ([]wrappedSkill, error) {
			resp, err := connection.Client.ListAgentSkills(params.QueryOptions())
			if err != nil {
				return nil, err
			}
			return wrap(resp.Results), nil
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.AgentSkillRead},
					XPermissions: []types.Capability{types.AgentSkillRead},
				},
			},
			DefaultColumns: []string{
				"CommonFields.ID",
				"CommonFields.Name",
				"CommonFields.Description",
				"BodyBytes",
			},
		})
}

// upload creates a skill from a markdown file. Name and description given
// on the command line override the file's front matter.
func upload() action.Pair {
	nameField := scaffoldcreate.FieldName(singular)
	nameField.Required = false
	nameField.Flag.Usage = "name for the skill, overriding the file's front matter (default: the file name)"
	descField := scaffoldcreate.FieldDescription(singular)
	descField.Flag.Usage = "description for the skill, overriding the file's front matter"
	pathField := scaffoldcreate.FieldPath("skill", true)
	pathField.Flag.Usage = "path to the markdown file to upload"
	pathField.Order = 100

	return scaffoldcreate.NewCreateAction(singular,
		map[string]scaffoldcreate.Field{
			"path":   pathField,
			"name":   nameField,
			"desc":   descField,
			"labels": scaffoldcreate.FieldLabels(),
		},
		func(fields map[string]scaffoldcreate.Field, _ *pflag.FlagSet) (id any, invalid string, err error) {
			sf, err := skillFromFile(strings.TrimSpace(fields["path"].Provider.Get()))
			if err != nil {
				return nil, err.Error(), nil
			}
			if name := strings.TrimSpace(fields["name"].Provider.Get()); name != "" {
				sf.Name = name
			}
			if desc := fields["desc"].Provider.Get(); desc != "" {
				sf.Description = desc
			}
			created, err := connection.Client.CreateAgentSkill(types.AgentSkill{
				CommonFields: types.CommonFields{
					Name:        sf.Name,
					Description: sf.Description,
					Labels:      scaffoldcreate.GetLabelsFromField(fields["labels"]),
				},
				Body: sf.Body,
			})
			if err != nil {
				return nil, "", err
			}
			return phrases.SuccessfullyCreatedItem(singular, created.ID), "", nil
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:  "upload",
				Long: "Upload a markdown file as a new agent skill.",
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.AgentSkillWrite},
					XPermissions: []types.Capability{types.AgentSkillWrite},
				},
			},
			IDIsSuccessMessage: true,
		})
}

// replace overwrites an existing skill's body (and, when the file's front
// matter names them, its name and description) from a file.
func replace() action.Pair {
	return scaffoldselect.NewSelectAction("replace a skill's content from a file",
		"Replace the body of an existing skill with a markdown file's. The name and description are replaced too when the file's front matter sets them. "+
			"Ownership and sharing are untouched.",
		singular,
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListAgentSkills(types.QueryOptions{})
			if err != nil {
				return nil, err
			}
			return listitem.WrapAssets(lr.Results), nil
		},
		func(IDs []string, addtlFlags *pflag.FlagSet) ([]scaffold.Result, error) {
			if len(IDs) != 1 {
				clilog.Writer.Warn("exactly1 function with incorrect number of IDs made it to operate()",
					log.KV("count", len(IDs)), scaffold.IdentifyCaller())
				if len(IDs) == 0 {
					return nil, errors.New(phrases.Exactly1ArgRequired("skill ID"))
				}
			}
			path, err := addtlFlags.GetString(ft.Path.Name())
			clilog.GetFlag(err)
			if path = strings.TrimSpace(path); path == "" {
				return nil, phrases.ErrFlagIsRequired(ft.Path.Name())
			}
			sf, err := skillFromFile(path)
			if err != nil {
				return nil, err
			}
			p := types.AgentSkillPatch{Body: types.NewOptional(sf.Body)}
			if sf.FromFrontMatter {
				if sf.Name != "" {
					p.Name = types.NewOptional(sf.Name)
				}
				if sf.Description != "" {
					p.Description = types.NewOptional(sf.Description)
				}
			}
			updated, err := connection.Client.UpdateAgentSkill(IDs[0], p)
			if err != nil {
				if phrases.IsNotFoundErr(err) {
					return nil, phrases.ErrUnknownIdentifier(IDs[0], "skill ID")
				}
				return nil, err
			}
			return []scaffold.Result{{
				Output:  "replaced the content of skill " + updated.Name + " (" + updated.ID + ") from " + path,
				Success: true,
			}}, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "replace",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					ft.Path.Register(fs, "", "markdown file")
					return fs
				},
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.AgentSkillRead, types.AgentSkillWrite},
					XPermissions: []types.Capability{types.AgentSkillRead, types.AgentSkillWrite},
				},
			},
			Exactly1: true,
		})
}

// download writes a skill out as a markdown file with front matter, so it
// uploads again unchanged.
func download() action.Pair {
	return scaffoldselect.NewSelectAction("download a skill",
		"Download a skill as a markdown file (with a front matter block naming it) for use locally or to upload elsewhere.",
		singular,
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListAgentSkills(types.QueryOptions{})
			if err != nil {
				return nil, err
			}
			return listitem.WrapAssets(lr.Results), nil
		},
		func(IDs []string, addtlFlags *pflag.FlagSet) ([]scaffold.Result, error) {
			if len(IDs) != 1 {
				clilog.Writer.Warn("exactly1 function with incorrect number of IDs made it to operate()",
					log.KV("count", len(IDs)), scaffold.IdentifyCaller())
				if len(IDs) == 0 {
					return nil, errors.New(phrases.Exactly1ArgRequired("skill ID"))
				}
			}
			s, err := connection.Client.GetAgentSkill(IDs[0])
			if err != nil {
				if phrases.IsNotFoundErr(err) {
					return nil, phrases.ErrUnknownIdentifier(IDs[0], "skill ID")
				}
				return nil, err
			}
			doc := render(s)
			out, err := addtlFlags.GetString(ft.Output.Name())
			clilog.GetFlag(err)
			if out == "" {
				return []scaffold.Result{{Output: doc, Success: true}}, nil
			}
			f, err := os.Create(out)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			n, err := f.WriteString(doc)
			if err != nil {
				return nil, err
			}
			return []scaffold.Result{{Output: phrases.SuccessfullyWroteToFile(n, f.Name()), Success: true}}, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "download",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					ft.Output.Register(fs)
					return fs
				},
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.AgentSkillRead},
					XPermissions: []types.Capability{types.AgentSkillRead},
				},
			},
			Exactly1: true,
		})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction(singular,
		func(dryrun bool, id string, _ *pflag.FlagSet) error {
			if dryrun {
				_, err := connection.Client.GetAgentSkill(id)
				return err
			}
			return connection.Client.DeleteAgentSkill(id)
		},
		func(params scaffolddelete.DataParameters) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListAgentSkills(params.QueryOptions())
			if err != nil {
				return nil, err
			}
			return listitem.WrapAssets(lr.Results), nil
		}, scaffolddelete.Options{
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.AgentSkillRead, types.AgentSkillWrite},
					XPermissions: []types.Capability{types.AgentSkillWrite},
				},
			},
			QueryOptionsFlags: scaffold.QOInclude{Everything: true},
		})
}
