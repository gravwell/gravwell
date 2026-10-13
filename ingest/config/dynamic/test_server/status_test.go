/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"uuid"

	"github.com/gravwell/gravwell/v4/hosted/plugins/tester"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic/server"
)

// testerKind is the real plugin these tests drive.  Using the actual Tester config rather
// than a stand in is the point: the failure being checked for is one only that plugin's
// own Verify can produce.
const testerKind = `Tester`

// testerRunner builds the definition a webserver would hold for a Tester runner.
func testerRunner(t *testing.T, id uuid.UUID, name, interval string) dynamic.RunnerDefinition {
	t.Helper()
	rd, err := dynamic.MapRunnerDefinition(testerKind, name, tester.Config{
		Ingester_UUID: id.String(),
		Tag_Name:      `test`,
		Interval:      interval,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rd.UUID != id {
		t.Fatalf("definition UUID = %v, want %v", rd.UUID, id)
	}
	return rd
}

// statusOf finds what one ingester last said about one runner.
func statusOf(t *testing.T, h *harness, runner uuid.UUID) (server.StatusRow, bool) {
	t.Helper()
	rows, err := h.store.RunnerStatuses(runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		return server.StatusRow{}, false
	}
	return rows[0], true
}

// TestTesterBadIntervalReportsUp is the case the whole status path exists for.
//
// An Interval of "3" is a perfectly good string, survives JSON, renders into a valid INI
// block and parses back out of one.  Nothing short of the plugin's own Verify can tell
// that it is not a duration, so this is the end to end proof that the ingester runs that
// Verify and that the answer gets back to the webserver.
func TestTesterBadIntervalReportsUp(t *testing.T) {
	h := newHarness(t)
	ingester := uuid.New()
	m, storage := newManager(t, h, ingester, `edge`)

	if err := m.RegisterKind(testerKind, false, tester.Config{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, `the Tester registration to reach the server`, func() bool {
		kinds, err := h.store.IngesterKinds(ingester)
		return err == nil && len(kinds) == 1 && kinds[0].Kind == testerKind
	})

	runner := uuid.New()

	// a runner that works, so that there is something to protect when it later breaks
	if err := h.store.PutRunner(testerRunner(t, runner, `beat`, `1s`)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, `the good runner to be written`, func() bool {
		ents, _ := os.ReadDir(storage)
		return len(ents) == 1
	})
	waitFor(t, `the good runner to report clean`, func() bool {
		st, ok := statusOf(t, h, runner)
		return ok && st.OK()
	})

	st, _ := statusOf(t, h, runner)
	if st.Ingester != ingester {
		t.Errorf("status came from %v, want %v", st.Ingester, ingester)
	}
	if st.Kind != testerKind || st.Name != `beat` {
		t.Errorf("status does not name the runner: %+v", st)
	}
	if st.Updated.IsZero() || st.Since.IsZero() {
		t.Errorf("status carries no timestamps: %+v", st)
	}
	good, err := os.ReadFile(onlyFile(t, storage))
	if err != nil {
		t.Fatal(err)
	}

	// now break it the way an operator would, a duration with no unit
	if err := h.store.PutRunner(testerRunner(t, runner, `beat`, `3`)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, `the broken interval to be reported`, func() bool {
		st, ok := statusOf(t, h, runner)
		return ok && !st.OK()
	})

	st, _ = statusOf(t, h, runner)
	// the plugin's own words, not ours
	if !strings.Contains(st.Error, `missing unit in duration`) {
		t.Errorf("the reported error does not carry the plugin's reason: %q", st.Error)
	}
	if st.Ingester != ingester {
		t.Errorf("the failure is attributed to %v, want %v", st.Ingester, ingester)
	}

	// and the configuration that worked is still on disk and still running.  Pulling it
	// out from under a running ingester because someone saved a typo would be worse than
	// the typo.
	still, err := os.ReadFile(onlyFile(t, storage))
	if err != nil {
		t.Fatal(err)
	}
	if string(still) != string(good) {
		t.Errorf("a rejected configuration replaced the working one on disk:\n%s", still)
	}

	// fix it, and the error should clear itself without anything having to retract it
	if err := h.store.PutRunner(testerRunner(t, runner, `beat`, `5s`)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, `the fix to clear the error`, func() bool {
		st, ok := statusOf(t, h, runner)
		return ok && st.OK()
	})
	if updated, err := os.ReadFile(onlyFile(t, storage)); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(updated), `5s`) {
		t.Errorf("the fixed configuration was not written:\n%s", updated)
	}
}

// TestTesterBadTagReportsUp is the case that was getting through.
//
// A tag is just a string on the wire and in the INI, so nothing before the plugin's own
// Verify can know that "bad tag" is one the indexer will refuse.  Before tag validation
// the configuration was accepted, written, and only failed later at tag negotiation,
// where nothing ties the failure back to the configuration that caused it.
func TestTesterBadTagReportsUp(t *testing.T) {
	h := newHarness(t)
	ingester := uuid.New()
	m, storage := newManager(t, h, ingester, `edge`)

	if err := m.RegisterKind(testerKind, false, tester.Config{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, `the Tester registration to reach the server`, func() bool {
		kinds, err := h.store.IngesterKinds(ingester)
		return err == nil && len(kinds) == 1
	})

	runner := uuid.New()
	good := testerRunner(t, runner, `beat`, `1s`)
	if err := h.store.PutRunner(good); err != nil {
		t.Fatal(err)
	}
	waitFor(t, `the good runner to report clean`, func() bool {
		st, ok := statusOf(t, h, runner)
		return ok && st.OK()
	})
	onDisk, err := os.ReadFile(onlyFile(t, storage))
	if err != nil {
		t.Fatal(err)
	}

	// every one of these is a tag the indexer refuses at negotiation
	for _, tag := range []string{`bad tag`, `bad!tag`, `bad.tag`, `bad/tag`, `bad*tag`} {
		t.Run(tag, func(t *testing.T) {
			bad := testerRunner(t, runner, `beat`, `1s`)
			bad = setTag(t, bad, tag)
			if err := h.store.PutRunner(bad); err != nil {
				t.Fatal(err)
			}
			waitFor(t, `the bad tag to be reported`, func() bool {
				st, ok := statusOf(t, h, runner)
				return ok && !st.OK()
			})
			st, _ := statusOf(t, h, runner)
			if !strings.Contains(strings.ToLower(st.Error), `tag`) {
				t.Errorf("the reported error does not say it is about a tag: %q", st.Error)
			}
			if !strings.Contains(st.Error, tag) {
				t.Errorf("the reported error does not name the offending tag %q: %q", tag, st.Error)
			}
			if st.Ingester != ingester {
				t.Errorf("attributed to %v, want %v", st.Ingester, ingester)
			}
			// and the configuration that worked is still the one on disk
			still, err := os.ReadFile(onlyFile(t, storage))
			if err != nil {
				t.Fatal(err)
			}
			if string(still) != string(onDisk) {
				t.Errorf("a rejected tag replaced the working config on disk:\n%s", still)
			}

			// put it back and the error clears
			if err := h.store.PutRunner(good); err != nil {
				t.Fatal(err)
			}
			waitFor(t, `the fix to clear the error`, func() bool {
				st, ok := statusOf(t, h, runner)
				return ok && st.OK()
			})
		})
	}
}

// setTag overwrites the Tag-Name variable the way an operator editing a form would.
func setTag(t *testing.T, rd dynamic.RunnerDefinition, tag string) dynamic.RunnerDefinition {
	t.Helper()
	out := rd
	out.Variables = append([]dynamic.Variable(nil), rd.Variables...)
	for i := range out.Variables {
		if out.Variables[i].Name == `Tag-Name` {
			out.Variables[i].Value = tag
			return out
		}
	}
	t.Fatalf("no Tag-Name variable in %+v", rd.Variables)
	return out
}

// TestTesterBadIntervalRefusesPush covers the other half: a push is answered with the
// objection, so a bad save is refused where the operator is standing rather than only
// turning up in a list they have to go and look at.
func TestTesterBadIntervalRefusesPush(t *testing.T) {
	h := newHarness(t)
	ingester := uuid.New()
	m, _ := newManager(t, h, ingester, `edge`)
	if err := m.RegisterKind(testerKind, false, tester.Config{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, `the Tester registration to reach the server`, func() bool {
		kinds, err := h.store.IngesterKinds(ingester)
		return err == nil && len(kinds) == 1
	})
	waitFor(t, `the ingester to be tracked as connected`, func() bool {
		_, body := h.get(t, `/ui/status`)
		return strings.Contains(body, `1 ingester connected`)
	})

	runner := uuid.New()
	form := url.Values{}
	form.Set(`kind`, testerKind)
	form.Set(`name`, `beat`)
	form.Set(`uuid`, runner.String())
	form.Set(`var.Tag-Name`, `test`)
	form.Set(`var.Interval`, `3`)

	code, body, _ := h.post(t, `/ui/save`, form)
	if code != http.StatusOK {
		t.Fatalf("POST /ui/save = %d: %s", code, body)
	}
	if !strings.Contains(body, `rejected it`) {
		t.Errorf("the save did not surface the rejection: %s", body)
	}
	if !strings.Contains(body, `missing unit in duration`) {
		t.Errorf("the save did not surface the reason: %s", body)
	}

	// and it is on the record too, not just in the response the operator happened to see
	waitFor(t, `the rejection to be stored`, func() bool {
		st, ok := statusOf(t, h, runner)
		return ok && !st.OK()
	})
}

// onlyFile asserts the directory holds exactly one file and returns its path.
func onlyFile(t *testing.T, dir string) string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		names := make([]string, 0, len(ents))
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Fatalf("expected exactly one config in %s, got %v", dir, names)
	}
	return dir + string(os.PathSeparator) + ents[0].Name()
}
