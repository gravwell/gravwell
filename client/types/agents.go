/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// NodeType is the discriminator that selects which concrete node a NodeSpec
// builds. It is the "Type" field a user sets on every node.
type NodeType string

const (
	// NodeSingle is one LLM tool-calling loop.
	NodeSingle NodeType = "single"
	// NodeSerial runs Children in order.
	NodeSerial NodeType = "serial"
	// NodeParallel runs Children concurrently.
	NodeParallel NodeType = "parallel"
	// NodeLoop runs its single child until it calls the exit tool.
	NodeLoop NodeType = "loop"
	// NodeRouter has an LLM pick one of Children to handle the turn.
	NodeRouter NodeType = "router"
	// NodeTool makes one deterministic tool call, with no model in it.
	NodeTool NodeType = "tool"
)

// SubagentOutput values: what a client sees while a subagent a node called
// is working. See NodeSpec.SubagentOutput.
const (
	SubagentCollapsed = "collapsed"
	SubagentShown     = "shown"
	SubagentHidden    = "hidden"
)

var (
	ErrNodeMissingName        = errors.New("node has no Name")
	ErrNodeMissingType        = errors.New("node has no Type")
	ErrNodeUnknownType        = errors.New("unknown node type")
	ErrNodeSingleHasChildren  = errors.New("a single node cannot have children")
	ErrNodeNoChildren         = errors.New("node needs at least one child")
	ErrNodeLoopChildCount     = errors.New("a loop node needs exactly one child")
	ErrNodeNilChild           = errors.New("child is null")
	ErrNodeDuplicateChildName = errors.New("duplicate child name")
	ErrNodeToolHasChildren    = errors.New("a tool node cannot have children")
	ErrNodeToolMissingTool    = errors.New("a tool node needs a Tool to call")
)

// NodeSpec is a single node in an agent graph.
type NodeSpec struct {
	// Type selects the concrete node to build. Required.
	Type NodeType

	// Name identifies the node. It must be unique among the children of a
	// Parallel or Router, which address their children by name. Required.
	Name string

	// Description is what a model is given to choose this node by: when it is
	// a child of a Router, the routing LLM reads it, and when it is a subagent
	// (an installed agent another node calls as a tool) it is the description
	// of that tool. Optional otherwise.
	Description string

	// Inherit, when true, gives this node (and, for composites, the subtree
	// beneath it) whatever tool set the parent passed down, and AllowedTools is
	// ignored. When false, the node gets exactly AllowedTools.
	Inherit bool

	// AllowedTools is the set of tools this node (and, for composites, the
	// subtree beneath it) may use when Inherit is false: those named here that
	// are also present in the parent's set. An empty list grants none. Filtering
	// composes down the tree, never widening it.
	//
	// Neither reaches into a subagent. A subagent is a separate installed
	// agent, built from its own spec against the full tool set the caller was
	// granted, because delegating to an agent runs that agent as its author
	// defined it (see AllowedSubagents).
	AllowedTools []string `json:",omitempty"`

	// AllowedSubagents names the installed agents this node's model may call
	// as tools, drawn from the agents the caller is allowed to see. Each named
	// agent becomes one tool, addressed by the agent's name and described by
	// its description, taking the task to carry out.
	//
	// Delegation is opt-in and does not inherit: an empty or absent list means
	// this node delegates to nothing. Names that match no installed agent are
	// dropped, as are an agent already on the delegation chain (so no agent
	// can be reached twice down one path, and none can call itself) and one
	// whose name is not usable as a tool name. Only a single node delegates;
	// the composites ignore this field.
	AllowedSubagents []string `json:",omitempty"`

	// SubagentOutput is what a client sees while a subagent this node called
	// is working: "collapsed" (the default, and the empty value) streams it as
	// a block the client can draw folded, "shown" asks the client to open it,
	// and "hidden" sends none of it.
	SubagentOutput string `json:",omitempty"`

	// Prompt is the system prompt for LLM-backed nodes (single, router). For a
	// router it guides the routing decision and is not handed to the chosen
	// child. Ignored by pure composites (serial, parallel, loop).
	Prompt string

	// MaxIterations caps the tool-calling loop for a single, or the number of
	// iterations for a loop.
	MaxIterations int

	// Tool is the tool a tool node calls, by name. Required for that type and
	// ignored by every other.
	Tool string `json:",omitempty"`

	// Arguments are the arguments a tool node calls its tool with. A value is
	// used literally unless it is a placeholder (${last_code_block},
	// ${last_message}, ${code_blocks}) that takes its text from the
	// conversation at the moment the node runs.
	Arguments map[string]string `json:",omitempty"`

	// Requires turns a tool node into a verification: a result that does not
	// contain this text is a failure. A verifying node vetoes the exit of the
	// loop it sits in, so the pass runs again with the failures in the
	// conversation, and marks the final answer.
	Requires string `json:",omitempty"`

	// MaxItems caps how many items a verifying tool node accepts. Items past
	// the cap are removed from the answer without being checked, and the
	// answer says so.
	MaxItems int `json:",omitempty"`

	// ForEach names the argument a tool node iterates, which makes it call its
	// tool once per item rather than once in total. The only source is
	// ${code_blocks}: every fenced block the stage above it produced.
	ForEach string `json:",omitempty"`

	// Children are the sub-nodes for composite types. Serial, parallel, and
	// router take one or more; a loop takes exactly one (the node it wraps). A
	// single and a tool node have none: what a single calls on demand is named
	// in AllowedSubagents and lives outside this graph.
	Children []*NodeSpec `json:",omitempty"`
}

// Validate checks the structure of the node and every node beneath it: each
// has a name and a known type, singles and tool nodes have no children, a tool
// node names its tool, the composites have the right number of children, and
// siblings have distinct names. Errors name the offending node by its path
// from this one.
func (n *NodeSpec) Validate() error {
	return n.validate(n.Name)
}

func (n *NodeSpec) validate(path string) error {
	if n.Name == "" {
		return fmt.Errorf("%s: %w", path, ErrNodeMissingName)
	}
	switch n.Type {
	case NodeSingle:
		if len(n.Children) != 0 {
			return fmt.Errorf("%s: %w", path, ErrNodeSingleHasChildren)
		}
	case NodeTool:
		if len(n.Children) != 0 {
			return fmt.Errorf("%s: %w", path, ErrNodeToolHasChildren)
		}
		if n.Tool == "" {
			return fmt.Errorf("%s: %w", path, ErrNodeToolMissingTool)
		}
	case NodeSerial, NodeParallel, NodeRouter:
		if len(n.Children) == 0 {
			return fmt.Errorf("%s: %s %w", path, n.Type, ErrNodeNoChildren)
		}
	case NodeLoop:
		if len(n.Children) != 1 {
			return fmt.Errorf("%s: %w, got %d", path, ErrNodeLoopChildCount, len(n.Children))
		}
	case "":
		return fmt.Errorf("%s: %w", path, ErrNodeMissingType)
	default:
		return fmt.Errorf("%s: %w %q", path, ErrNodeUnknownType, n.Type)
	}
	seen := make(map[string]bool, len(n.Children))
	for i, c := range n.Children {
		if c == nil {
			return fmt.Errorf("%s: child %d %w", path, i, ErrNodeNilChild)
		}
		if seen[c.Name] {
			return fmt.Errorf("%s: %w %q", path, ErrNodeDuplicateChildName, c.Name)
		}
		seen[c.Name] = true
		if err := c.validate(path + "/" + c.Name); err != nil {
			return err
		}
	}
	return nil
}

// Clone deep-copies the node and its subtree.
func (n *NodeSpec) Clone() *NodeSpec {
	if n == nil {
		return nil
	}
	c := *n
	c.AllowedTools = slices.Clone(n.AllowedTools)
	c.AllowedSubagents = slices.Clone(n.AllowedSubagents)
	c.Arguments = maps.Clone(n.Arguments)
	c.Children = nil
	for _, ch := range n.Children {
		c.Children = append(c.Children, ch.Clone())
	}
	return &c
}

// Agent is a stored AI agent: a graph of nodes with a named entrypoint, plus
// the images that represent it. The LLM, tool set, and tool handler are
// supplied by the webserver at runtime.
type Agent struct {
	CommonFields

	Entrypoint NodeSpec

	// StateDiagram is an SVG of the agent's state diagram.
	StateDiagram string

	// Avatar is an SVG used as the agent's avatar.
	Avatar string
}

// AgentPatch is the type used to request an update to an existing Agent.
type AgentPatch struct {
	CommonFieldsPatch
	Entrypoint   Optional[NodeSpec] `json:",omitzero"`
	StateDiagram Optional[string]   `json:",omitzero"`
	Avatar       Optional[string]   `json:",omitzero"`
}

// ToPatch converts a into an AgentPatch with every field set.
func (a Agent) ToPatch() AgentPatch {
	return AgentPatch{
		CommonFieldsPatch: a.CommonFields.ToPatch(),
		Entrypoint:        NewOptional(a.Entrypoint),
		StateDiagram:      NewOptional(a.StateDiagram),
		Avatar:            NewOptional(a.Avatar),
	}
}

type AgentListResponse = ListResponse[Agent]

// AgentSkill is a markdown document describing a skill an AI agent can load:
// its Name and Description (from CommonFields) are advertised to the agent,
// and Body is the skill content itself.
type AgentSkill struct {
	CommonFields

	Body string
}

// AgentSkillPatch is the type used to request an update to an existing AgentSkill.
type AgentSkillPatch struct {
	CommonFieldsPatch
	Body Optional[string] `json:",omitzero"`
}

// ToPatch converts s into an AgentSkillPatch with every field set.
func (s AgentSkill) ToPatch() AgentSkillPatch {
	return AgentSkillPatch{
		CommonFieldsPatch: s.CommonFields.ToPatch(),
		Body:              NewOptional(s.Body),
	}
}

type AgentSkillListResponse = ListResponse[AgentSkill]
