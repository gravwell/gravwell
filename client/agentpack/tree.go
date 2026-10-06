/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package agentpack reads and writes AI agent definitions in the forms that
// live outside the webserver: the JSON blob the agents API exchanges, and a
// directory tree that keeps each piece of an agent in its own file so the
// definition can be edited with ordinary tools and kept in version control.
//
// The tree layout is:
//
//	metadata.json          the agent's Name and Description, and its asset
//	                       fields (ID, owner, sharing, labels) when it has them
//	state.svg              StateDiagram (required when the reader demands it)
//	avatar.svg             Avatar (optional)
//	entrypoint/            the entrypoint NodeSpec
//	    metadata.json      every NodeSpec field except Prompt and Children
//	    prompt             the node's Prompt, verbatim
//	    <child>/           one directory per child, same layout, recursively
//
// A node's metadata.json carries its children as an ordered list of directory
// names rather than as nested NodeSpecs: the order of Children is meaningful
// (a serial node runs them in sequence) and a directory listing is not, so the
// order has to be written down somewhere. Everything else in the file is the
// NodeSpec as it appears in the agent JSON.
//
// An image may also be dropped in as state.png or avatar.png, which ReadTree
// wraps in an SVG; WriteTree always writes .svg.
package agentpack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
)

// Fixed names within the tree. PromptFile and MetadataFile are reserved
// inside every node directory, so a child node can never be given either name.
const (
	MetadataFile  = "metadata.json"
	PromptFile    = "prompt"
	EntrypointDir = "entrypoint"
	StateFile     = "state.svg"
	AvatarFile    = "avatar.svg"
)

var (
	ErrMissingName      = errors.New("agent has no Name")
	ErrTreeExists       = errors.New("an unpacked agent is already there: remove it first, or unpack into an empty directory")
	ErrMissingState     = errors.New("an agent needs a state diagram")
	ErrPromptInMetadata = errors.New("a node's prompt belongs in its prompt file, not in its metadata")
)

// agentMetadata is the top level metadata.json: the parts of an Agent that
// are neither the node graph nor an image.
type agentMetadata struct {
	resourceFields
	Name        string `json:"Name"`
	Description string `json:"Description,omitempty"`
}

// resourceFields are the parts of an Agent's CommonFields that belong to it
// as a stored asset (identity, ownership, sharing, labels) rather than to the
// definition of its graph. An agent fetched from the agents API carries them,
// so they have to round-trip, but each is left out when unset: an agent
// written by hand has none of them, and zero IDs would only be noise. The
// server-derived fields (timestamps, Owner, LastModifiedBy, Can, Version) are
// not kept; the API sets them.
type resourceFields struct {
	ID       string    `json:"ID,omitzero"`
	ParentID string    `json:"ParentID,omitzero"`
	OwnerID  int32     `json:"OwnerID,omitzero"`
	Readers  types.ACL `json:"Readers,omitzero"`
	Writers  types.ACL `json:"Writers,omitzero"`
	Labels   []string  `json:"Labels,omitzero"`
	Kit      string    `json:"Kit,omitzero"`
}

func resourceOf(a *types.Agent) resourceFields {
	return resourceFields{
		ID:       a.ID,
		ParentID: a.ParentID,
		OwnerID:  a.OwnerID,
		Readers:  a.Readers,
		Writers:  a.Writers,
		Labels:   a.Labels,
		Kit:      a.KitID,
	}
}

// applyTo copies the resource fields onto an agent.
func (r resourceFields) applyTo(a *types.Agent) {
	a.ID, a.ParentID, a.OwnerID = r.ID, r.ParentID, r.OwnerID
	a.Readers, a.Writers, a.Labels, a.KitID = r.Readers, r.Writers, r.Labels, r.Kit
}

// nodeMetadata is a node's metadata.json: the whole NodeSpec, with the
// fields that live somewhere other than this file overridden. Embedding rather
// than restating the spec means a field added to NodeSpec later round-trips
// through pack and unpack without this package being touched.
//
// Each override has to repeat the embedded field's JSON name to take effect.
// encoding/json settles a name conflict in favour of the shallower field, but
// it discards a `json:"-"` field before that comparison happens — tagging an
// override "-" would leave the embedded field promoted and serialized anyway,
// which would write the prompt into metadata.json alongside its own file.
type nodeMetadata struct {
	types.NodeSpec

	// Prompt lives in the node's prompt file. This override exists only to
	// keep the embedded field out of the JSON, and is always empty on write.
	Prompt string `json:"Prompt,omitempty"`

	// Children are the names of the subdirectories holding this node's
	// children, in the order gaf will run them.
	Children []string `json:"Children,omitempty"`
}

// ------------------------------------------------------------------ blob

// Decode parses an agent JSON blob. Decoding is strict: an unknown field
// would otherwise be dropped silently on the way through, and the point of
// a round trip is fidelity. The node graph and the images are checked.
func Decode(b []byte) (*types.Agent, error) {
	var agent types.Agent
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&agent); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("trailing data after the agent JSON")
	}
	if err := CheckAgent(&agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

// CheckAgent validates what an agent definition must get right before the
// webserver would accept it: a sound node graph and clean SVG images. It does
// not require a name; callers that need one check for it themselves.
func CheckAgent(a *types.Agent) error {
	if err := a.Entrypoint.Validate(); err != nil {
		return fmt.Errorf("invalid node graph: %w", err)
	}
	for field, svg := range map[string]string{"StateDiagram": a.StateDiagram, "Avatar": a.Avatar} {
		if svg == "" {
			continue
		}
		if err := CheckSVG(svg); err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
	}
	return nil
}

// outAgent mirrors Agent for output only, so the asset fields (see
// resourceFields) are emitted only when set.
type outAgent struct {
	resourceFields
	Name         string         `json:"Name"`
	Description  string         `json:"Description,omitempty"`
	Entrypoint   types.NodeSpec `json:"Entrypoint"`
	StateDiagram string         `json:"StateDiagram,omitempty"`
	Avatar       string         `json:"Avatar,omitempty"`
}

// Encode renders the agent blob. The round trip through a generic value is
// what sorts the object keys: Go orders map keys but not struct fields, and
// agent JSON kept in version control is key-sorted, so re-encoding an agent
// produces a diff of what actually changed rather than of every line.
// UseNumber keeps MaxIterations an integer instead of a float.
func Encode(a *types.Agent) ([]byte, error) {
	out := outAgent{
		resourceFields: resourceOf(a),
		Name:           a.Name,
		Description:    a.Description,
		Entrypoint:     *a.Entrypoint.Clone(),
		StateDiagram:   a.StateDiagram,
		Avatar:         a.Avatar,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encoding agent JSON: %w", err)
	}
	var generic any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&generic); err != nil {
		return nil, fmt.Errorf("encoding agent JSON: %w", err)
	}
	sorted, err := json.MarshalIndent(generic, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding agent JSON: %w", err)
	}
	return append(sorted, '\n'), nil
}

// ------------------------------------------------------------------ write

// WriteTree writes an agent out as the directory tree under dir, creating dir
// if needed. It refuses to write over an existing tree: that would silently
// mix the new agent's nodes with whatever the old one left behind, and any
// edits in there are the user's. The graph is not validated; callers that
// care do so first.
func WriteTree(dir string, agent *types.Agent) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating %q: %w", dir, err)
	}
	epPath := filepath.Join(dir, EntrypointDir)
	if _, err := os.Stat(epPath); err == nil {
		return fmt.Errorf("%q: %w", epPath, ErrTreeExists)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking %q: %w", epPath, err)
	}

	if err := writeJSON(filepath.Join(dir, MetadataFile), agentMetadata{
		resourceFields: resourceOf(agent),
		Name:           agent.Name,
		Description:    agent.Description,
	}); err != nil {
		return err
	}
	if err := writeSVG(filepath.Join(dir, StateFile), agent.StateDiagram, "StateDiagram"); err != nil {
		return err
	}
	if err := writeSVG(filepath.Join(dir, AvatarFile), agent.Avatar, "Avatar"); err != nil {
		return err
	}
	return writeNode(epPath, &agent.Entrypoint)
}

// writeNode writes one node's directory and recurses into its children.
func writeNode(path string, node *types.NodeSpec) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("creating %q: %w", path, err)
	}

	// Pick each child's directory name up front so the parent's metadata can
	// record them in order.
	used := map[string]bool{MetadataFile: true, PromptFile: true}
	names := make([]string, 0, len(node.Children))
	for _, child := range node.Children {
		names = append(names, SafeName(child.Name, used))
	}

	meta := nodeMetadata{
		NodeSpec: *node,
		Children: names,
	}
	// The embedded copy's own versions of the overridden fields are shadowed
	// and never written, but clear them anyway so nothing downstream mistakes
	// this for a complete NodeSpec.
	meta.NodeSpec.Prompt = ""
	meta.NodeSpec.Children = nil
	if err := writeJSON(filepath.Join(path, MetadataFile), meta); err != nil {
		return err
	}

	pp := filepath.Join(path, PromptFile)
	if err := os.WriteFile(pp, []byte(node.Prompt), 0644); err != nil {
		return fmt.Errorf("writing %q: %w", pp, err)
	}

	for i, child := range node.Children {
		if err := writeNode(filepath.Join(path, names[i]), child); err != nil {
			return err
		}
	}
	return nil
}

// SafeName turns a node or agent name into a file name that is safe on disk
// and unique among the names already in used (which it updates), reducing
// anything outside a conservative set to an underscore. A node's real name
// lives in its metadata.json, so mangling the directory name loses nothing.
func SafeName(name string, used map[string]bool) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	// Trimming dots keeps us clear of "", ".", ".." and hidden directories.
	out := strings.Trim(b.String(), ".")
	if out == "" {
		out = "node"
	}
	// Case-insensitive uniqueness, since the tree may well land on a
	// case-insensitive filesystem.
	base := out
	for i := 2; used[strings.ToLower(out)]; i++ {
		out = fmt.Sprintf("%s-%d", base, i)
	}
	used[strings.ToLower(out)] = true
	return out
}

// writeSVG writes an image field from the agent out as an .svg file, checking
// that it is a clean SVG. An empty field writes nothing.
func writeSVG(path, svg, field string) error {
	if svg == "" {
		return nil
	}
	if err := CheckSVG(svg); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	if err := os.WriteFile(path, []byte(svg), 0644); err != nil {
		return fmt.Errorf("writing %q: %w", path, err)
	}
	return nil
}

// ------------------------------------------------------------------- read

// ReadTree reads an unpacked directory tree back into an Agent. Every node is
// validated as it is read, so an error names the directory to fix.
// requireState makes a missing state diagram an error.
func ReadTree(dir string, requireState bool) (*types.Agent, error) {
	var meta agentMetadata
	if err := readJSON(filepath.Join(dir, MetadataFile), &meta); err != nil {
		return nil, err
	}
	if meta.Name == "" {
		return nil, fmt.Errorf("%q: %w", filepath.Join(dir, MetadataFile), ErrMissingName)
	}

	agent := types.Agent{CommonFields: types.CommonFields{Name: meta.Name, Description: meta.Description}}
	meta.resourceFields.applyTo(&agent)

	var err error
	if agent.StateDiagram, err = readImage(filepath.Join(dir, StateFile), requireState); err != nil {
		return nil, err
	}
	if agent.Avatar, err = readImage(filepath.Join(dir, AvatarFile), false); err != nil {
		return nil, err
	}

	epPath := filepath.Join(dir, EntrypointDir)
	if fi, err := os.Stat(epPath); err != nil {
		return nil, fmt.Errorf("entrypoint: %w", err)
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", epPath)
	}
	node, err := readNode(epPath)
	if err != nil {
		return nil, err
	}
	agent.Entrypoint = *node
	return &agent, nil
}

// readNode reads one node directory and recurses into the child directories
// its metadata names.
func readNode(path string) (*types.NodeSpec, error) {
	var meta nodeMetadata
	if err := readJSON(filepath.Join(path, MetadataFile), &meta); err != nil {
		return nil, err
	}

	// A prompt in the metadata would be silently dropped in favour of the
	// prompt file, so say where it belongs instead.
	if meta.Prompt != "" {
		return nil, fmt.Errorf("%q: %w", filepath.Join(path, MetadataFile), ErrPromptInMetadata)
	}

	pp := filepath.Join(path, PromptFile)
	prompt, err := os.ReadFile(pp)
	if err != nil {
		return nil, fmt.Errorf("reading prompt: %w", err)
	}

	// Start from the embedded spec, so any NodeSpec field this package does not
	// name still survives the trip, then fill in the ones kept out of the JSON.
	node := meta.NodeSpec
	node.Prompt = string(prompt)
	node.Children = nil

	seen := make(map[string]bool, len(meta.Children))
	for _, name := range meta.Children {
		// Children name sibling directories and nothing else; a path here
		// would let a hand-edited metadata.json reach outside the tree.
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			return nil, fmt.Errorf("%q: %q is not a valid child directory name", filepath.Join(path, MetadataFile), name)
		}
		if seen[name] {
			return nil, fmt.Errorf("%q: child %q listed twice", filepath.Join(path, MetadataFile), name)
		}
		seen[name] = true
		child, err := readNode(filepath.Join(path, name))
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, child)
	}

	// A directory that no parent points at would be dropped on the floor, so
	// say so rather than quietly losing a node someone just added.
	if err := checkStrayDirs(path, seen); err != nil {
		return nil, err
	}

	// Checked here rather than over the finished tree so the error names the
	// directory the user has to go and edit: a node's Name and its directory
	// name need not match, since the name is sanitized on the way to disk.
	if err := CheckNode(&node, path); err != nil {
		return nil, err
	}
	return &node, nil
}

// checkStrayDirs reports subdirectories of a node directory that its metadata
// does not list as children.
func checkStrayDirs(path string, listed map[string]bool) error {
	ents, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("reading %q: %w", path, err)
	}
	var stray []string
	for _, ent := range ents {
		if !ent.IsDir() || listed[ent.Name()] {
			continue
		}
		stray = append(stray, ent.Name())
	}
	if len(stray) == 0 {
		return nil
	}
	sort.Strings(stray)
	quoted := make([]string, len(stray))
	for i, s := range stray {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return fmt.Errorf("%q: directories %s are not listed in the Children of %q; add them there to include them",
		path, strings.Join(quoted, ", "), filepath.Join(path, MetadataFile))
}

// readImage reads an image file for the agent blob as an SVG. The .svg file
// at path is used if present; failing that, a PNG beside it (state.png for
// state.svg) is wrapped in an SVG, so a tree can be given raster art. Having
// both is an error, since one would be silently ignored. A required image must
// be there. An optional one may be missing or empty, either of which yields
// an empty string and leaves the field out of the blob. Whatever is there has
// to be a clean SVG or a valid PNG, so nothing corrupt or scripted is embedded.
func readImage(path string, required bool) (string, error) {
	pngPath := strings.TrimSuffix(path, ".svg") + ".png"
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		path = pngPath
		raw, err = os.ReadFile(path)
	} else if _, perr := os.Stat(pngPath); perr == nil {
		return "", fmt.Errorf("both %q and %q exist: remove one", path, pngPath)
	}
	if err != nil {
		if os.IsNotExist(err) && !required {
			return "", nil
		}
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%q does not exist: %w", strings.TrimSuffix(path, ".png")+".svg", ErrMissingState)
		}
		return "", fmt.Errorf("reading %q: %w", path, err)
	}
	if len(raw) == 0 && !required {
		return "", nil
	}
	svg, err := SVGFromFile(raw)
	if err != nil {
		return "", fmt.Errorf("%q: %w", path, err)
	}
	return svg, nil
}

// ----------------------------------------------------------------- shared

// CheckNode applies the structural rules of a node graph to one node only
// (its children are not descended into), so a bad node is reported against
// the path the caller knows it by. path is only ever used for the message.
func CheckNode(node *types.NodeSpec, path string) error {
	if node.Name == "" {
		return fmt.Errorf("%s: node has no Name", path)
	}
	switch node.Type {
	case types.NodeSingle:
		if len(node.Children) != 0 {
			return fmt.Errorf("%s: a single node cannot have children", path)
		}
	case types.NodeSerial, types.NodeParallel, types.NodeRouter:
		if len(node.Children) == 0 {
			return fmt.Errorf("%s: a %s node needs at least one child", path, node.Type)
		}
	case types.NodeLoop:
		if len(node.Children) != 1 {
			return fmt.Errorf("%s: a loop node needs exactly one child, got %d", path, len(node.Children))
		}
	case "":
		return fmt.Errorf("%s: node has no Type", path)
	default:
		return fmt.Errorf("%s: unknown node type %q", path, node.Type)
	}

	// Parallel and Router address their children by name, so a collision would
	// silently lose one.
	seen := make(map[string]bool, len(node.Children))
	for i, child := range node.Children {
		if child == nil {
			return fmt.Errorf("%s: child %d is null", path, i)
		}
		if seen[child.Name] {
			return fmt.Errorf("%s: duplicate child name %q", path, child.Name)
		}
		seen[child.Name] = true
	}
	return nil
}

// readJSON decodes a metadata file, rejecting unknown fields so a typo in a
// hand-edited file is an error rather than a setting that silently does
// nothing.
func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %q: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("parsing %q: %w", path, err)
	}
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %q: %w", path, err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0644); err != nil {
		return fmt.Errorf("writing %q: %w", path, err)
	}
	return nil
}
