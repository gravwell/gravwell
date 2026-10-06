/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"strings"
	"testing"
	"uuid"

	"github.com/gravwell/gravwell/v4/hosted/plugins/tester"
)

// TestKindMetadataReachesTheServer is the whole path for a plugin's description: the
// ingester registers a real plugin, its icon, version and documentation ride along with
// the registration, the server stores them, and the interface draws them.
func TestKindMetadataReachesTheServer(t *testing.T) {
	h := newHarness(t)
	ingester := uuid.New()
	m, _ := newManager(t, h, ingester, `edge`)

	if err := m.RegisterKind(testerKind, false, tester.Config{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, `the registration to reach the server`, func() bool {
		kinds, err := h.store.IngesterKinds(ingester)
		return err == nil && len(kinds) == 1
	})

	// stored, as part of the definition that is the record of record
	kinds, err := h.store.IngesterKinds(ingester)
	if err != nil {
		t.Fatal(err)
	}
	md := kinds[0].Metadata
	if md == nil {
		t.Fatal(`the registration carried no metadata`)
	}
	if md.Icon != tester.Icon {
		t.Errorf("icon did not survive the round trip: %d bytes, want %d", len(md.Icon), len(tester.Icon))
	}
	if got := md.Version.String(); got != `1.0.0` {
		t.Errorf("version = %q, want 1.0.0", got)
	}
	if !md.Version.Enabled() {
		t.Error(`a declared version should report Enabled`)
	}
	if len(md.Documentation) != 1 || md.Documentation[0].Name != `source` {
		t.Errorf("documentation did not survive: %+v", md.Documentation)
	}

	// and reachable by kind, which is how a runner that carries no metadata gets an icon
	byKind, err := h.store.KindMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if byKind[testerKind] == nil || byKind[testerKind].Icon != tester.Icon {
		t.Error(`KindMetadata did not hand back the icon`)
	}

	// the menu draws it: sanitized markup, the version, and the docs link
	_, body := h.get(t, `/ui/kinds`)
	for _, must := range []string{`<svg `, `class="icon"`, `M9.8 8 V12.6`, `v1.0.0`, `source`,
		`https://github.com/gravwell/gravwell/tree/main/hosted/plugins/tester`} {
		if !strings.Contains(body, must) {
			t.Errorf("/ui/kinds is missing %q:\n%s", must, body)
		}
	}
	// what it must never contain, whatever a plugin ships
	for _, never := range []string{`<script`, `onload=`, `c2pa`} {
		if strings.Contains(strings.ToLower(body), never) {
			t.Errorf("/ui/kinds carries %q", never)
		}
	}

	// a configured runner carries no metadata of its own, so the list has to find the
	// icon through its kind
	runner := uuid.New()
	if err := h.store.PutRunner(testerRunner(t, runner, `beat`, `1s`)); err != nil {
		t.Fatal(err)
	}
	stored, err := h.store.Runner(runner)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Metadata != nil {
		t.Error(`a runner built from a form should carry no metadata of its own`)
	}
	_, body = h.get(t, `/ui/runners`)
	if !strings.Contains(body, `class="icon"`) || !strings.Contains(body, `M9.8 8 V12.6`) {
		t.Errorf("the runner list did not pick up its kind's icon:\n%s", body)
	}

	// and so does the form
	_, body = h.get(t, `/ui/edit?uuid=`+runner.String())
	for _, must := range []string{`kindhead`, `class="icon"`, `v1.0.0`, `source`} {
		if !strings.Contains(body, must) {
			t.Errorf("the form is missing %q:\n%s", must, body)
		}
	}
}

// TestIngesterListCarriesAnIcon covers the glyph on the connection dropdown.  It is the
// interface's own markup rather than anything a plugin ships, and it tracks the
// connection state so a row reads without having to pick out the dot beside it.
func TestIngesterListCarriesAnIcon(t *testing.T) {
	h := newHarness(t)
	ingester := uuid.New()
	m, _ := newManager(t, h, ingester, `edge`)
	if err := m.RegisterKind(testerKind, false, tester.Config{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, `the ingester to be recorded`, func() bool {
		known, err := h.store.Ingesters()
		return err == nil && len(known) == 1
	})
	waitFor(t, `the ingester to show as connected`, func() bool {
		_, body := h.get(t, `/ui/ingesters`)
		return strings.Contains(body, `icon ing on`)
	})

	code, body := h.get(t, `/ui/ingesters`)
	if code != 200 {
		t.Fatalf("GET /ui/ingesters = %d", code)
	}
	// one glyph per ingester, to the left of the UUID it belongs to
	if n := strings.Count(body, `class="icon ing`); n != 1 {
		t.Errorf("drew %d ingester glyphs for 1 ingester:\n%s", n, body)
	}
	idx, uidx := strings.Index(body, `<svg class="icon ing`), strings.Index(body, ingester.String())
	if idx < 0 || uidx < 0 || idx > uidx {
		t.Errorf("the glyph is not to the left of the ingester it belongs to:\n%s", body)
	}
	// it is ours, so it is never routed through the sanitizer and must not be escaped
	if !strings.Contains(body, `<svg class="icon ing on"`) {
		t.Errorf("the glyph was not rendered as markup:\n%s", body)
	}

	// once the ingester goes away the glyph says so, the same as the dot
	m.Close()
	waitFor(t, `the disconnect to show`, func() bool {
		_, b := h.get(t, `/ui/ingesters`)
		return strings.Contains(b, `icon ing off`)
	})
}

// TestKindWithoutMetadataStillDraws covers a plugin that describes nothing, which is the
// normal case for anything that has not had an icon drawn for it yet.
func TestKindWithoutMetadataStillDraws(t *testing.T) {
	h := newHarness(t)
	ingester := uuid.New()
	m, _ := newManager(t, h, ingester, `edge`)
	if err := m.RegisterKind(`testplugin`, false, pluginConfig{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, `the registration to reach the server`, func() bool {
		kinds, err := h.store.IngesterKinds(ingester)
		return err == nil && len(kinds) == 1
	})
	kinds, err := h.store.IngesterKinds(ingester)
	if err != nil {
		t.Fatal(err)
	}
	if kinds[0].Metadata != nil {
		t.Errorf("a plugin that describes nothing should carry no metadata: %+v", kinds[0].Metadata)
	}
	byKind, err := h.store.KindMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := byKind[`testplugin`]; ok {
		t.Error(`a plugin with no metadata should be absent from the map, not present and empty`)
	}
	// the menu still lists it, by name, with no icon and no version
	_, body := h.get(t, `/ui/kinds`)
	if !strings.Contains(body, `testplugin`) {
		t.Errorf("a plugin with no metadata vanished from the menu:\n%s", body)
	}
	if strings.Contains(body, `class="icon"`) {
		t.Errorf("an icon was drawn for a plugin that ships none:\n%s", body)
	}
	if strings.Contains(body, `class="ver"`) {
		t.Errorf("a version was drawn for a plugin that declares none:\n%s", body)
	}
}
