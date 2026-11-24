/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package agents

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gravwell/gravwell/v4/client/agentpack"
	"github.com/gravwell/gravwell/v4/client/types"
)

var errNoName = errors.New("the agent has no name; set one in the file or with --name")

// agentFromFile reads an agent definition (the JSON form of types.Agent, as
// gaftool writes and as download emits) and returns the definition fields
// ready to be sent to the webserver. Server-managed fields in the file (ID,
// owner, timestamps, ...) are dropped so an agent downloaded from one
// deployment can be uploaded to another; sharing (Readers/Writers) is kept.
//
// Decoding (agentpack.Decode) is strict so a typo in a hand-written file is
// an error here rather than a 400 from the webserver, and the node graph and
// images are checked the same way the webserver will.
func agentFromFile(path string) (types.Agent, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return types.Agent{}, err
	}
	return decodeAgent(b)
}

func decodeAgent(b []byte) (types.Agent, error) {
	in, err := agentpack.Decode(b)
	if err != nil {
		return types.Agent{}, fmt.Errorf("the file is not an agent definition: %w", err)
	}
	return types.Agent{
		Name:         in.Name,
		Description:  in.Description,
		Labels:       in.Labels,
		Readers:      in.Readers,
		Writers:      in.Writers,
		Entrypoint:   in.Entrypoint,
		StateDiagram: in.StateDiagram,
		Avatar:       in.Avatar,
	}, nil
}

// definitionPatch is the patch that replaces an existing agent's definition
// with a's, leaving ownership and sharing as they are on the server.
func definitionPatch(a types.Agent) types.AgentPatch {
	p := types.AgentPatch{
		Entrypoint:   types.NewOptional(a.Entrypoint),
		StateDiagram: types.NewOptional(a.StateDiagram),
		Avatar:       types.NewOptional(a.Avatar),
	}
	if a.Name != "" {
		p.Name = types.NewOptional(a.Name)
	}
	if a.Description != "" {
		p.Description = types.NewOptional(a.Description)
	}
	if a.Labels != nil {
		p.Labels = types.NewOptional(a.Labels)
	}
	return p
}

// countNodes returns the number of nodes in the graph rooted at n.
func countNodes(n *types.NodeSpec) int {
	if n == nil {
		return 0
	}
	count := 1
	for _, c := range n.Children {
		count += countNodes(c)
	}
	return count
}

// renderGraph describes an agent for the show action: its identity, then
// one line per node, indented by depth.
func renderGraph(a types.Agent) string {
	var sb strings.Builder
	sb.WriteString(a.Name + " (" + a.ID + ")\n")
	if a.Description != "" {
		sb.WriteString(a.Description + "\n")
	}
	writeNode(&sb, &a.Entrypoint, 0)
	return strings.TrimSuffix(sb.String(), "\n")
}

func writeNode(sb *strings.Builder, n *types.NodeSpec, depth int) {
	if n == nil {
		return
	}
	sb.WriteString(strings.Repeat("  ", depth) + "- " + string(n.Type) + " " + n.Name)
	var attrs []string
	if n.Inherit {
		attrs = append(attrs, "inherits tools")
	} else if len(n.AllowedTools) == 0 {
		attrs = append(attrs, "no tools")
	}
	if len(n.AllowedTools) > 0 {
		attrs = append(attrs, "tools: "+strings.Join(n.AllowedTools, ", "))
	}
	if n.MaxIterations > 0 {
		attrs = append(attrs, "max iterations: "+strconv.Itoa(n.MaxIterations))
	}
	if n.Prompt != "" {
		attrs = append(attrs, "prompt: "+strconv.Itoa(len(n.Prompt))+" bytes")
	}
	if len(attrs) > 0 {
		sb.WriteString(" [" + strings.Join(attrs, "; ") + "]")
	}
	sb.WriteString("\n")
	for _, c := range n.Children {
		writeNode(sb, c, depth+1)
	}
}

// exampleDefinition is a small but complete agent definition, shown by the
// json action.
const exampleDefinition = `{
  "Name": "triage",
  "Description": "Looks at an alert and says what to do about it.",
  "Labels": ["ai"],
  "Entrypoint": {
    "Type": "serial",
    "Name": "triage",
    "Inherit": true,
    "Children": [
      {
        "Type": "single",
        "Name": "investigate",
        "Prompt": "You are the investigation stage. Use the tools to gather the facts.",
        "AllowedTools": ["run_query", "list_tags"],
        "MaxIterations": 20
      },
      {
        "Type": "single",
        "Name": "report",
        "Prompt": "You are the reporting stage. Summarize the findings above as Markdown."
      }
    ]
  },
  "StateDiagram": "<svg xmlns=\"http://www.w3.org/2000/svg\">...</svg>",
  "Avatar": "<svg xmlns=\"http://www.w3.org/2000/svg\">...</svg>"
}`
