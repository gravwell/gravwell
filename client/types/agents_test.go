/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func testAgent() Agent {
	return Agent{
		CommonFields: CommonFields{
			ID:          "abc",
			Name:        "triage",
			Description: "Triage incoming alerts",
			Labels:      []string{"ai"},
		},
		Entrypoint: NodeSpec{
			Type: NodeRouter, Name: "root", Prompt: "route it", Inherit: true,
			Children: []*NodeSpec{
				{Type: NodeSingle, Name: "a", Description: "does a", Prompt: "be a"},
				{Type: NodeLoop, Name: "b", Description: "does b", MaxIterations: 3, Inherit: true, Children: []*NodeSpec{
					{Type: NodeSingle, Name: "inner", Prompt: "p", AllowedTools: []string{"run_query"}},
				}},
			},
		},
		StateDiagram: `<svg xmlns="http://www.w3.org/2000/svg"/>`,
		Avatar:       `<svg xmlns="http://www.w3.org/2000/svg"><circle r="1"/></svg>`,
	}
}

func TestAgentJSONRoundTrip(t *testing.T) {
	a := testAgent()
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var got Agent
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, a) {
		t.Errorf("round trip changed the agent:\ngot  %+v\nwant %+v", got, a)
	}
	if !got.Entrypoint.Inherit || got.Entrypoint.Children[0].Inherit {
		t.Errorf("Inherit lost: %s", b)
	}
}

func TestAgentToPatch(t *testing.T) {
	a := testAgent()
	p := a.ToPatch()
	if !p.Entrypoint.IsSet() || !reflect.DeepEqual(p.Entrypoint.Value(), a.Entrypoint) {
		t.Errorf("Entrypoint not carried into the patch")
	}
	if p.StateDiagram.Value() != a.StateDiagram || p.Avatar.Value() != a.Avatar {
		t.Errorf("images not carried into the patch")
	}
	if p.Name.Value() != a.Name {
		t.Errorf("common fields not carried into the patch")
	}
	// An unset field is left out on the wire.
	b, err := json.Marshal(AgentPatch{Avatar: NewOptional("x")})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["Entrypoint"]; ok {
		t.Errorf("unset Entrypoint emitted: %s", b)
	}
	if _, ok := m["Avatar"]; !ok {
		t.Errorf("set Avatar not emitted: %s", b)
	}
}

func TestNodeSpecValidate(t *testing.T) {
	valid := testAgent()
	if err := valid.Entrypoint.Validate(); err != nil {
		t.Errorf("valid graph rejected: %v", err)
	}
	single := func(name string) *NodeSpec { return &NodeSpec{Type: NodeSingle, Name: name} }
	bad := map[string]struct {
		n    NodeSpec
		want error
	}{
		"no name":         {NodeSpec{Type: NodeSingle}, ErrNodeMissingName},
		"no type":         {NodeSpec{Name: "x"}, ErrNodeMissingType},
		"unknown type":    {NodeSpec{Type: "bogus", Name: "x"}, ErrNodeUnknownType},
		"single children": {NodeSpec{Type: NodeSingle, Name: "x", Children: []*NodeSpec{single("a")}}, ErrNodeSingleHasChildren},
		"serial empty":    {NodeSpec{Type: NodeSerial, Name: "x"}, ErrNodeNoChildren},
		"loop two":        {NodeSpec{Type: NodeLoop, Name: "x", Children: []*NodeSpec{single("a"), single("b")}}, ErrNodeLoopChildCount},
		"nil child":       {NodeSpec{Type: NodeParallel, Name: "x", Children: []*NodeSpec{nil}}, ErrNodeNilChild},
		"dup child":       {NodeSpec{Type: NodeRouter, Name: "x", Children: []*NodeSpec{single("a"), single("a")}}, ErrNodeDuplicateChildName},
		"nested":          {NodeSpec{Type: NodeSerial, Name: "x", Children: []*NodeSpec{{Type: NodeLoop, Name: "l"}}}, ErrNodeLoopChildCount},
	}
	for name, tc := range bad {
		if err := tc.n.Validate(); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", name, err, tc.want)
		}
	}
}

func TestNodeSpecClone(t *testing.T) {
	a := testAgent()
	c := a.Entrypoint.Clone()
	if !reflect.DeepEqual(*c, a.Entrypoint) {
		t.Fatalf("clone differs")
	}
	c.Children[1].Children[0].AllowedTools[0] = "changed"
	c.Children[0].Name = "changed"
	if a.Entrypoint.Children[1].Children[0].AllowedTools[0] != "run_query" || a.Entrypoint.Children[0].Name != "a" {
		t.Errorf("clone shares memory with the original")
	}
}

func TestAgentSkillJSONRoundTrip(t *testing.T) {
	s := AgentSkill{
		CommonFields: CommonFields{ID: "abc", Name: "triage", Description: "Triage incoming alerts"},
		Body:         "# Triage\n\n1. Look at the alert\n",
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var got AgentSkill
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, s) {
		t.Errorf("round trip changed the skill:\ngot  %+v\nwant %+v", got, s)
	}
	p := s.ToPatch()
	if p.Body.Value() != s.Body || p.Name.Value() != s.Name {
		t.Errorf("ToPatch dropped fields: %+v", p)
	}
}
