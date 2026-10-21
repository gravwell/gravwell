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
)

// NodeSpec is a single node in an agent graph.
type NodeSpec struct {
	// Type selects the concrete node to build. Required.
	Type NodeType

	// Name identifies the node. It must be unique among the children of a
	// Parallel or Router, which address their children by name. Required.
	Name string

	// Description is shown to the routing LLM when this node is a child of a
	// Router, so it should describe what the node handles. Optional otherwise.
	Description string

	// Inherit, when true, gives this node (and, for composites, the subtree
	// beneath it) whatever tool set the parent passed down, and AllowedTools is
	// ignored. When false, the node gets exactly AllowedTools.
	Inherit bool

	// AllowedTools is the set of tools this node (and, for composites, the
	// subtree beneath it) may use when Inherit is false: those named here that
	// are also present in the parent's set. An empty list grants none. Filtering
	// composes down the tree, never widening it.
	AllowedTools []string `json:",omitempty"`

	// Prompt is the system prompt for LLM-backed nodes (single, router). For a
	// router it guides the routing decision and is not handed to the chosen
	// child. Ignored by pure composites (serial, parallel, loop).
	Prompt string

	// MaxIterations caps the tool-calling loop for a single, or the number of
	// iterations for a loop.
	MaxIterations int

	// Children are the sub-nodes for composite types. Serial, parallel, and
	// router take one or more; a loop takes exactly one (the node it wraps).
	Children []*NodeSpec `json:",omitempty"`
}

// Validate checks the structure of the node and every node beneath it: each
// has a name and a known type, singles have no children, the other composites
// have the right number, and siblings have distinct names. Errors name the
// offending node by its path from this one.
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

type AgentListResponse struct {
	BaseListResponse
	Results []Agent
}

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

type AgentSkillListResponse struct {
	BaseListResponse
	Results []AgentSkill
}
