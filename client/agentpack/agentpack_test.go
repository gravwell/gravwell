/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package agentpack

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
)

const tinySVG = `<svg xmlns="http://www.w3.org/2000/svg" width="2" height="2"><rect width="2" height="2"/></svg>`

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// testAgent is a small but complete agent: a router over a single and a loop.
func testAgent() *types.Agent {
	return &types.Agent{
		CommonFields: types.CommonFields{Name: "test-agent", Description: "for tests"},
		StateDiagram: tinySVG,
		Entrypoint: types.NodeSpec{
			Type: types.NodeRouter, Name: "root", Prompt: "route it", Inherit: true,
			Children: []*types.NodeSpec{
				{Type: types.NodeSingle, Name: "a", Description: "does a", Prompt: "# A\nbe **a**"},
				{Type: types.NodeLoop, Name: "b", Description: "does b", Inherit: true, Children: []*types.NodeSpec{
					{Type: types.NodeSingle, Name: "inner", Prompt: "p", AllowedTools: []string{"run_query"}, MaxIterations: 4},
				}},
			},
		},
	}
}

func TestCheckSVG(t *testing.T) {
	ok := []string{
		`<svg xmlns="http://www.w3.org/2000/svg"/>`,
		`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><a href="#x"><rect/></a></svg>`,
	}
	for _, s := range ok {
		if err := CheckSVG(s); err != nil {
			t.Errorf("CheckSVG(%q) = %v, want nil", s, err)
		}
	}
	bad := map[string]error{
		``:                        ErrNotSVG,
		`not xml`:                 ErrNotSVG,
		`<svg>no namespace</svg>`: ErrNotSVG,
		`<html xmlns="http://www.w3.org/2000/svg"/>`:                                                ErrNotSVG,
		`<svg xmlns="http://www.w3.org/2000/svg">`:                                                  ErrMalformedSVG,
		`<!DOCTYPE svg [<!ENTITY a "a">]><svg xmlns="http://www.w3.org/2000/svg"/>`:                 ErrNotSVG,
		`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`:                   ErrActiveSVG,
		`<svg xmlns="http://www.w3.org/2000/svg"><foreignObject/></svg>`:                            ErrActiveSVG,
		`<svg xmlns="http://www.w3.org/2000/svg" onLoad="alert(1)"/>`:                               ErrActiveSVG,
		`<svg xmlns="http://www.w3.org/2000/svg"><a href=" java script:alert(1)"/></svg>`:           ErrActiveSVG,
		`<svg xmlns="http://www.w3.org/2000/svg"><image href="data:image/svg+xml;base64,x"/></svg>`: ErrActiveSVG,
	}
	for s, want := range bad {
		if err := CheckSVG(s); !errors.Is(err, want) {
			t.Errorf("CheckSVG(%q) = %v, want %v", s, err, want)
		}
	}
}

func TestSVGFromPNG(t *testing.T) {
	raw := tinyPNG(t)
	svg, err := SVGFromPNG(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckSVG(svg); err != nil {
		t.Errorf("wrapped PNG is not a clean SVG: %v", err)
	}
	if !strings.Contains(svg, `viewBox="0 0 3 2"`) || !strings.Contains(svg, base64.StdEncoding.EncodeToString(raw)) {
		t.Errorf("wrapped PNG lost its size or pixels: %s", svg)
	}
	if _, err := SVGFromPNG([]byte("\x89PNG\r\n\x1a\ntruncated")); err == nil {
		t.Errorf("corrupt PNG wrapped")
	}
	if s, err := SVGFromFile([]byte(tinySVG)); err != nil || s != tinySVG {
		t.Errorf("SVGFromFile on an SVG: %q %v", s, err)
	}
	if _, err := SVGFromFile([]byte("neither")); err == nil {
		t.Errorf("SVGFromFile accepted junk")
	}
}

// An agent fetched from the agents API carries its asset fields; unpacking
// and packing it again must hand back the same agent.
func TestTreeRoundTrip(t *testing.T) {
	a := testAgent()
	a.Avatar = tinySVG
	a.ID = "4f0c4f2e"
	a.OwnerID = 7
	a.Readers = types.ACL{GIDs: []int32{1, 2}, Global: true}
	a.Writers = types.ACL{GIDs: []int32{2}}
	a.Labels = []string{"triage"}
	a.KitID = types.NewNullable("io.gravwell.triage")

	dir := filepath.Join(t.TempDir(), "tree")
	if err := WriteTree(dir, a); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{MetadataFile, StateFile, AvatarFile, EntrypointDir + "/" + MetadataFile, EntrypointDir + "/" + PromptFile, EntrypointDir + "/a/" + PromptFile, EntrypointDir + "/b/inner/" + MetadataFile} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("tree lacks %s: %v", f, err)
		}
	}
	if err := WriteTree(dir, a); !errors.Is(err, ErrTreeExists) {
		t.Errorf("writing over a tree: %v", err)
	}
	got, err := ReadTree(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, a) {
		t.Errorf("round trip changed the agent:\ngot  %+v\nwant %+v", got, a)
	}
	// And through the blob.
	b, err := Encode(got)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, a) {
		t.Errorf("blob round trip changed the agent:\ngot  %+v\nwant %+v", again, a)
	}
}

// A hand-written definition has no asset fields, and encoding it must not
// invent zero-valued ones; the keys come out sorted.
func TestEncodeOmitsUnsetFields(t *testing.T) {
	b, err := Encode(testAgent())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ID", "ParentID", "OwnerID", "Readers", "Writers", "Labels", "KitID", "CreatedAt", "Owner", "Can", "Version", "Avatar"} {
		if _, ok := m[key]; ok {
			t.Errorf("encoded agent has unset %s: %s", key, b)
		}
	}
	if !strings.HasPrefix(string(b), "{\n  \"Description\"") {
		t.Errorf("keys not sorted: %.40s", b)
	}
	if bytes.Contains(b, []byte(`"MaxIterations": 4.0`)) || !bytes.Contains(b, []byte(`"MaxIterations": 4`)) {
		t.Errorf("MaxIterations not kept an integer: %s", b)
	}
}

func TestDecodeRejects(t *testing.T) {
	bad := map[string]string{
		"not json":      `nope`,
		"unknown field": `{"Name":"x","Bogus":1,"Entrypoint":{"Type":"single","Name":"x"}}`,
		"trailing data": `{"Name":"x","Entrypoint":{"Type":"single","Name":"x"}} {}`,
		"bad graph":     `{"Name":"x","Entrypoint":{"Type":"loop","Name":"x"}}`,
		"scripted svg":  `{"Name":"x","Entrypoint":{"Type":"single","Name":"x"},"Avatar":"<svg xmlns=\"http://www.w3.org/2000/svg\" onload=\"x()\"/>"}`,
	}
	for name, in := range bad {
		if _, err := Decode([]byte(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Decode([]byte(`{"Name":"x","Entrypoint":{"Type":"loop","Name":"x"}}`)); !errors.Is(err, types.ErrNodeLoopChildCount) {
		t.Errorf("bad graph error does not carry the cause: %v", err)
	}
}

// A tree may be given a PNG in place of an SVG, but not both, and a required
// state diagram must be there.
func TestReadTreeImages(t *testing.T) {
	dir := t.TempDir()
	if err := WriteTree(dir, testAgent()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, StateFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.png"), tinyPNG(t), 0644); err != nil {
		t.Fatal(err)
	}
	a, err := ReadTree(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.StateDiagram, "data:image/png;base64,") {
		t.Errorf("state.png not wrapped into StateDiagram: %q", a.StateDiagram)
	}
	if err := os.WriteFile(filepath.Join(dir, StateFile), []byte(tinySVG), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTree(dir, true); err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("state.svg beside state.png not refused: %v", err)
	}
	os.Remove(filepath.Join(dir, "state.png"))
	os.Remove(filepath.Join(dir, StateFile))
	if _, err := ReadTree(dir, true); !errors.Is(err, ErrMissingState) {
		t.Errorf("missing state diagram: %v", err)
	}
	if a, err := ReadTree(dir, false); err != nil || a.StateDiagram != "" {
		t.Errorf("optional state diagram: %+v %v", a, err)
	}
}

// Hand edits that would silently lose something are refused by name.
func TestReadTreeHandEdits(t *testing.T) {
	fresh := func(t *testing.T) string {
		dir := t.TempDir()
		if err := WriteTree(dir, testAgent()); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	t.Run("stray directory", func(t *testing.T) {
		dir := fresh(t)
		os.MkdirAll(filepath.Join(dir, EntrypointDir, "orphan"), 0755)
		if _, err := ReadTree(dir, true); err == nil || !strings.Contains(err.Error(), "orphan") {
			t.Errorf("stray directory not reported: %v", err)
		}
	})
	t.Run("prompt in metadata", func(t *testing.T) {
		dir := fresh(t)
		p := filepath.Join(dir, EntrypointDir, "a", MetadataFile)
		os.WriteFile(p, []byte(`{"Type":"single","Name":"a","Prompt":"here"}`), 0644)
		if _, err := ReadTree(dir, true); !errors.Is(err, ErrPromptInMetadata) {
			t.Errorf("prompt in metadata: %v", err)
		}
	})
	t.Run("escaping child", func(t *testing.T) {
		dir := fresh(t)
		p := filepath.Join(dir, EntrypointDir, MetadataFile)
		os.WriteFile(p, []byte(`{"Type":"router","Name":"root","Children":["../a"]}`), 0644)
		if _, err := ReadTree(dir, true); err == nil || !strings.Contains(err.Error(), "not a valid child") {
			t.Errorf("escaping child: %v", err)
		}
	})
	t.Run("broken graph names the directory", func(t *testing.T) {
		dir := fresh(t)
		p := filepath.Join(dir, EntrypointDir, "b", MetadataFile)
		os.WriteFile(p, []byte(`{"Type":"loop","Name":"b","Inherit":true,"Children":[]}`), 0644)
		os.RemoveAll(filepath.Join(dir, EntrypointDir, "b", "inner"))
		if _, err := ReadTree(dir, true); err == nil || !strings.Contains(err.Error(), filepath.Join(EntrypointDir, "b")) {
			t.Errorf("broken graph: %v", err)
		}
	})
}

func TestSafeName(t *testing.T) {
	used := map[string]bool{MetadataFile: true}
	cases := []struct{ in, want string }{
		{"stage a", "stage_a"},
		{"Stage A", "Stage_A-2"},
		{"../x", "_x"},
		{"...", "node"},
		{"metadata.json", "metadata.json-2"},
	}
	for _, c := range cases {
		if got := SafeName(c.in, used); got != c.want {
			t.Errorf("SafeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
