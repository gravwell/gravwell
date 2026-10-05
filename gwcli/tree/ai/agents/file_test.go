//go:build ci

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
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
)

func TestDecodeAgent(t *testing.T) {
	t.Run("example definition", func(t *testing.T) {
		a, err := decodeAgent([]byte(exampleDefinition))
		if err != nil {
			t.Fatalf("the json action's example does not decode: %v", err)
		}
		if a.Name != "triage" || a.Entrypoint.Type != types.NodeSerial || len(a.Entrypoint.Children) != 2 {
			t.Errorf("decoded %+v", a)
		}
		if !a.Entrypoint.Inherit || a.Entrypoint.Children[0].Inherit || len(a.Entrypoint.Children[0].AllowedTools) != 2 || len(a.Entrypoint.Children[1].AllowedTools) != 0 {
			t.Errorf("tool settings lost: %+v", a.Entrypoint)
		}
	})

	t.Run("server fields dropped, sharing kept", func(t *testing.T) {
		a, err := decodeAgent([]byte(`{"ID":"abc","OwnerID":7,"Version":3,"CreatedAt":"2026-01-01T00:00:00Z",
			"Name":"x","Labels":["l"],"Readers":{"Global":true},"Writers":{"GIDs":[2]},
			"Entrypoint":{"Type":"single","Name":"x","Prompt":"p"}}`))
		if err != nil {
			t.Fatal(err)
		}
		if a.ID != "" || a.OwnerID != 0 || a.Version != 0 || !a.CreatedAt.IsZero() {
			t.Errorf("server-managed fields kept: %+v", a.CommonFields)
		}
		if !a.Readers.Global || len(a.Writers.GIDs) != 1 || len(a.Labels) != 1 || a.Name != "x" {
			t.Errorf("definition fields lost: %+v", a.CommonFields)
		}
	})

	bad := map[string]string{
		"not json":      `nope`,
		"unknown field": `{"Name":"x","Bogus":1,"Entrypoint":{"Type":"single","Name":"x"}}`,
		"legacy png":    `{"Name":"x","Entrypoint":{"Type":"single","Name":"x"},"AvatarPNG":"abc"}`,
		"trailing data": `{"Name":"x","Entrypoint":{"Type":"single","Name":"x"}} {}`,
		"bad graph":     `{"Name":"x","Entrypoint":{"Type":"loop","Name":"x"}}`,
		"no node type":  `{"Name":"x","Entrypoint":{"Name":"x"}}`,
	}
	for name, in := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeAgent([]byte(in)); err == nil {
				t.Errorf("accepted")
			}
		})
	}
	if _, err := decodeAgent([]byte(`{"Name":"x","Entrypoint":{"Type":"loop","Name":"x"}}`)); !errors.Is(err, types.ErrNodeLoopChildCount) {
		t.Errorf("bad graph error does not carry the cause: %v", err)
	}
}

func TestDefinitionPatch(t *testing.T) {
	a, err := decodeAgent([]byte(exampleDefinition))
	if err != nil {
		t.Fatal(err)
	}
	p := definitionPatch(a)
	if !p.Entrypoint.IsSet() || !p.Name.IsSet() || !p.Description.IsSet() || !p.Labels.IsSet() {
		t.Errorf("definition fields not in the patch: %+v", p)
	}
	if p.OwnerID.IsSet() || p.Readers.IsSet() || p.Writers.IsSet() {
		t.Errorf("ownership or sharing in the patch: %+v", p)
	}
	// A file without a name or description leaves the server's alone.
	bare, err := decodeAgent([]byte(`{"Entrypoint":{"Type":"single","Name":"x","Prompt":"p"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if p := definitionPatch(bare); p.Name.IsSet() || p.Description.IsSet() || p.Labels.IsSet() || !p.Entrypoint.IsSet() {
		t.Errorf("bare definition patch: %+v", p)
	}
}

func TestCountNodes(t *testing.T) {
	a, _ := decodeAgent([]byte(exampleDefinition))
	if n := countNodes(&a.Entrypoint); n != 3 {
		t.Errorf("counted %d nodes, want 3", n)
	}
	if n := countNodes(nil); n != 0 {
		t.Errorf("counted %d nodes for nil, want 0", n)
	}
}

func TestWrap(t *testing.T) {
	a, _ := decodeAgent([]byte(exampleDefinition))
	w := wrap([]types.Agent{a})
	if len(w) != 1 || w[0].Type != types.NodeSerial || w[0].Nodes != 3 || w[0].Name != "triage" {
		t.Errorf("wrapped %+v", w)
	}
	if !strings.Contains(exampleDefinition, `"Type": "serial"`) {
		t.Errorf("example lost its serial entrypoint")
	}
}
