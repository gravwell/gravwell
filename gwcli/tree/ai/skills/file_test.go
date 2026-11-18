//go:build ci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
)

func TestParseSkill(t *testing.T) {
	t.Run("front matter", func(t *testing.T) {
		sf, err := parseSkill("---\nname: query-skills\nDescription:  How to query. \n\n---\n# Query skills\n\nbody\n")
		if err != nil {
			t.Fatal(err)
		}
		if sf.Name != "query-skills" || sf.Description != "How to query." || !sf.FromFrontMatter {
			t.Errorf("parsed %+v", sf)
		}
		if sf.Body != "# Query skills\n\nbody\n" {
			t.Errorf("body %q", sf.Body)
		}
	})
	t.Run("crlf and bom", func(t *testing.T) {
		sf, err := parseSkill("\uFEFF---\r\nname: x\r\n---\r\nbody\r\n")
		if err != nil {
			t.Fatal(err)
		}
		if sf.Name != "x" || sf.Body != "body\r\n" {
			t.Errorf("parsed %+v", sf)
		}
	})
	t.Run("no front matter", func(t *testing.T) {
		sf, err := parseSkill("# just a doc\n---\nnot front matter\n")
		if err != nil {
			t.Fatal(err)
		}
		if sf.Name != "" || sf.FromFrontMatter || sf.Body != "# just a doc\n---\nnot front matter\n" {
			t.Errorf("parsed %+v", sf)
		}
	})
	t.Run("unclosed", func(t *testing.T) {
		if _, err := parseSkill("---\nname: x\nbody\n"); err == nil {
			t.Errorf("accepted")
		}
	})
	t.Run("bad line", func(t *testing.T) {
		if _, err := parseSkill("---\nname x\n---\nbody\n"); err == nil {
			t.Errorf("accepted")
		}
	})
	t.Run("not utf8", func(t *testing.T) {
		if _, err := parseSkill("body \xff\n"); err == nil {
			t.Errorf("accepted")
		}
	})
}

func TestSkillFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "incident-response.md")
	if err := os.WriteFile(path, []byte("# IR\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sf, err := skillFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sf.Name != "incident-response" || sf.Body != "# IR\n" || sf.FromFrontMatter {
		t.Errorf("parsed %+v", sf)
	}
	if _, err := skillFromFile(filepath.Join(dir, "missing.md")); err == nil {
		t.Errorf("missing file accepted")
	}
}

func TestRenderRoundTrip(t *testing.T) {
	s := types.AgentSkill{
		CommonFields: types.CommonFields{Name: "triage", Description: "Triage alerts"},
		Body:         "# Triage\n\n1. look",
	}
	sf, err := parseSkill(render(s))
	if err != nil {
		t.Fatal(err)
	}
	if sf.Name != s.Name || sf.Description != s.Description || sf.Body != s.Body+"\n" || !sf.FromFrontMatter {
		t.Errorf("round trip changed the skill: %+v", sf)
	}
	noDesc := render(types.AgentSkill{CommonFields: types.CommonFields{Name: "x"}, Body: "b\n"})
	if noDesc != "---\nname: x\n---\nb\n" {
		t.Errorf("rendered %q", noDesc)
	}
}
