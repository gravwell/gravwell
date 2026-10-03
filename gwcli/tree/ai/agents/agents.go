/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package agents provides actions for managing AI agents: listing them, and
// uploading, replacing, downloading and deleting their definitions. An agent
// definition is a JSON file (see the json action for the shape); gaftool is
// the tool for building one, so there is no editor here.
package agents

import (
	"errors"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/agentpack"
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
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const singular = "agent"

// NewNav returns the agents nav.
func NewNav() *cobra.Command {
	return treeutils.GenerateNav("agents", "manage AI agents",
		"Agents are node graphs that drive the Gravwell AI. A single-node agent holds a conversation in the chat GUI; "+
			"fan-out agents (serial, parallel, loop, router) run non-interactively, for example from a flow's Logbot node.\n"+
			"Definitions are JSON files: build one with gaftool, upload it here, and download it again to keep it in version control.",
		nil,
		[]action.Pair{
			list(),
			upload(),
			replace(),
			download(),
			delete(),
			jsonAction(),
		}, treeutils.NodeOptions{CommandAliases: []string{"agent"}})
}

// wrappedAgent flattens an agent for the list view: the graph becomes its
// entrypoint type and node count.
type wrappedAgent struct {
	types.CommonFields

	Type  types.NodeType
	Nodes int
}

func wrap(agents []types.Agent) []wrappedAgent {
	out := make([]wrappedAgent, len(agents))
	for i, a := range agents {
		out[i] = wrappedAgent{
			CommonFields: a.CommonFields,
			Type:         a.Entrypoint.Type,
			Nodes:        countNodes(&a.Entrypoint),
		}
	}
	return out
}

func list() action.Pair {
	return scaffoldlist.NewListAction("list agents", "List the agents available to your user.",
		wrappedAgent{},
		func(fs *pflag.FlagSet, params scaffoldlist.DataParameters) ([]wrappedAgent, error) {
			resp, err := connection.Client.ListAgents(params.QueryOpts)
			if err != nil {
				return nil, err
			}
			return wrap(resp.Results), nil
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.AgentRead},
					XPermissions: []types.Capability{types.AgentRead},
				},
			},
			DefaultColumns: []string{
				"CommonFields.ID",
				"CommonFields.Name",
				"CommonFields.Description",
				"Type",
				"Nodes",
			},
		})
}

// upload creates an agent from a definition file. Name, description and
// labels given on the command line override the file's.
func upload() action.Pair {
	nameField := scaffoldcreate.FieldName(singular)
	nameField.Required = false
	nameField.Flag.Usage = "name for the agent, overriding the one in the file"
	descField := scaffoldcreate.FieldDescription(singular)
	descField.Flag.Usage = "description for the agent, overriding the one in the file"
	pathField := scaffoldcreate.FieldPath("agent definition", true)
	pathField.Flag.Usage = "path to the agent definition (JSON) to upload"
	pathField.Order = 100

	return scaffoldcreate.NewCreateAction(singular,
		map[string]scaffoldcreate.Field{
			"path":   pathField,
			"name":   nameField,
			"desc":   descField,
			"labels": scaffoldcreate.FieldLabels(),
		},
		func(fields map[string]scaffoldcreate.Field, _ *pflag.FlagSet) (id any, invalid string, err error) {
			a, err := agentFromFile(strings.TrimSpace(fields["path"].Provider.Get()))
			if err != nil {
				return nil, err.Error(), nil
			}
			if name := strings.TrimSpace(fields["name"].Provider.Get()); name != "" {
				a.Name = name
			}
			if desc := fields["desc"].Provider.Get(); desc != "" {
				a.Description = desc
			}
			if labels := scaffoldcreate.GetLabelsFromField(fields["labels"]); len(labels) > 0 {
				a.Labels = labels
			}
			if a.Name == "" {
				return nil, errNoName.Error(), nil
			}
			created, err := connection.Client.CreateAgent(a)
			if err != nil {
				return nil, "", err
			}
			return phrases.SuccessfullyCreatedItem(singular, created.ID), "", nil
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "upload",
				Long: "Upload an agent definition from a JSON file, creating a new agent.\n" +
					"Call " + stylesheet.Path(true, "~", "ai", "agents", "json") + " to see the format of the file; gaftool builds and packs them.",
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.AgentWrite},
					XPermissions: []types.Capability{types.AgentWrite},
				},
			},
			IDIsSuccessMessage: true,
		})
}

// replace overwrites an existing agent's definition with a file's, keeping
// its ID, ownership and sharing.
func replace() action.Pair {
	return scaffoldselect.NewSelectAction("replace an agent's definition from a file",
		"Replace the definition (node graph, images, name, description and labels) of an existing agent with the contents of a JSON file. "+
			"Ownership and sharing are untouched.",
		singular,
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListAgents(nil)
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
					return nil, errors.New(phrases.Exactly1ArgRequired("agent ID"))
				}
			}
			path, err := addtlFlags.GetString(ft.Path.Name())
			clilog.GetFlag(err)
			if path = strings.TrimSpace(path); path == "" {
				return nil, phrases.ErrFlagIsRequired(ft.Path.Name())
			}
			a, err := agentFromFile(path)
			if err != nil {
				return nil, err
			}
			updated, err := connection.Client.UpdateAgent(IDs[0], definitionPatch(a))
			if err != nil {
				if phrases.IsNotFoundErr(err) {
					return nil, phrases.ErrUnknownIdentifier(IDs[0], "agent ID")
				}
				return nil, err
			}
			return []scaffold.Result{{
				Output:  "replaced the definition of agent " + updated.Name + " (" + updated.ID + ") from " + path,
				Success: true,
			}}, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "replace",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					ft.Path.Register(fs, "", "agent definition (JSON) file")
					return fs
				},
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.AgentRead, types.AgentWrite},
					XPermissions: []types.Capability{types.AgentRead, types.AgentWrite},
				},
			},
			Exactly1: true,
		})
}

// download writes an agent's definition out as key-sorted JSON without the
// server-managed fields, suitable for upload, replace, gaftool and version
// control.
func download() action.Pair {
	return scaffoldselect.NewSelectAction("download an agent's definition",
		"Download the definition of an agent as JSON, for use with gaftool or to upload elsewhere.",
		singular,
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListAgents(nil)
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
					return nil, errors.New(phrases.Exactly1ArgRequired("agent ID"))
				}
			}
			a, err := connection.Client.GetAgent(IDs[0])
			if err != nil {
				if phrases.IsNotFoundErr(err) {
					return nil, phrases.ErrUnknownIdentifier(IDs[0], "agent ID")
				}
				return nil, err
			}
			b, err := agentpack.Encode(&a)
			if err != nil {
				return nil, err
			}
			out, err := addtlFlags.GetString(ft.Output.Name())
			clilog.GetFlag(err)
			if out == "" {
				return []scaffold.Result{{Output: string(b), Success: true}}, nil
			}
			f, err := os.Create(out)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			n, err := f.Write(b)
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
					IPermissions: []types.Capability{types.AgentRead},
					XPermissions: []types.Capability{types.AgentRead},
				},
			},
			Exactly1: true,
		})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction(singular,
		func(dryrun bool, id string, _ *pflag.FlagSet) error {
			if dryrun {
				_, err := connection.Client.GetAgent(id)
				return err
			}
			return connection.Client.DeleteAgent(id)
		},
		func(params scaffolddelete.DataParameters) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListAgents(params.QueryOpts)
			if err != nil {
				return nil, err
			}
			return listitem.WrapAssets(lr.Results), nil
		}, scaffolddelete.Options{
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.AgentRead, types.AgentWrite},
					XPermissions: []types.Capability{types.AgentWrite},
				},
			},
			QueryOptionsFlags: scaffold.QOInclude{Everything: true},
		})
}

func jsonAction() action.Pair {
	return scaffold.NewBasicAction("json", "display the agent definition format",
		"Print an example of the JSON an agent definition file holds. "+
			"Node types are single, serial, parallel, loop and router; only a lone single node can hold a conversation in the chat GUI. "+
			"A node with Inherit set uses its parent's tools; otherwise it gets exactly AllowedTools (none when left out). "+
			"StateDiagram and Avatar are SVG documents and may be omitted.",
		func(fs *pflag.FlagSet) (output string, addtlCmds tea.Cmd) {
			return exampleDefinition, nil
		},
		scaffold.BasicOptions{},
	)
}
