/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/hosted/plugins/sqs"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic/rpc"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic/server"
)

const testSecret = `a-shared-token-for-the-test-server`

// pluginConfig is a stand in for a real plugin config, covering every control the form
// has to draw.
type pluginConfig struct {
	Ingester_UUID string
	Tag_Name      string
	Page_Size     int
	Rate          float64
	Enabled       bool
	Sections      []string
	Token         string `json:"-" dynamic:"secret"`
	derived       int
}

// requiredPluginConfig exercises the required annotation on its own, so the shared
// fixture above stays optional for the tests that do not care.
type requiredPluginConfig struct {
	Ingester_UUID string
	Tag_Name      string `dynamic:"required"`
	Page_Size     int
	Token         string `json:"-" dynamic:"secret,required"`
}

// harness is the whole test server, stood up in process.
type harness struct {
	ts     *httptest.Server
	store  *Store
	theAPI *server.API
}

// api is the server side of the protocol, for the tests that drive it directly rather
// than through a route.
func (h *harness) api() *server.API { return h.theAPI }

func newHarness(t *testing.T) *harness {
	t.Helper()
	store := NewStore()
	h, api, err := NewServer(store, testSecret, nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return &harness{ts: ts, store: store, theAPI: api}
}

// dial connects as an ingester would.
func (h *harness) dial(t *testing.T, mux *rpc.Mux) *rpc.Session {
	t.Helper()
	ctx, cf := context.WithTimeout(context.Background(), 10*time.Second)
	defer cf()
	s, err := rpc.Dial(ctx, rpc.ClientConfig{
		Webserver:    h.ts.URL,
		Path:         client.INGESTERS_CONTROL_URL,
		Token:        testSecret,
		ID:           uuid.New(),
		Class:        `test`,
		Handlers:     mux,
		PingInterval: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// dialAs connects as an ingester carrying a class.
func (h *harness) dialAs(t *testing.T, class string) *rpc.Session {
	t.Helper()
	ctx, cf := context.WithTimeout(context.Background(), 10*time.Second)
	defer cf()
	s, err := rpc.Dial(ctx, rpc.ClientConfig{
		Webserver:    h.ts.URL,
		Path:         client.INGESTERS_CONTROL_URL,
		Token:        testSecret,
		ID:           uuid.New(),
		Class:        class,
		PingInterval: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func (h *harness) get(t *testing.T, pth string) (int, string) {
	t.Helper()
	resp, err := http.Get(h.ts.URL + pth)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b)
}

func (h *harness) post(t *testing.T, pth string, form url.Values) (int, string, http.Header) {
	t.Helper()
	resp, err := http.PostForm(h.ts.URL+pth, form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b), resp.Header
}

// proto is the registration an ingester would send for pluginConfig.
func proto(t *testing.T) dynamic.RunnerDefinition {
	t.Helper()
	rd, err := dynamic.MapRunnerDefinition(`testplugin`, `testplugin`, pluginConfig{})
	if err != nil {
		t.Fatal(err)
	}
	rd.Name = ``
	rd.UUID = uuid.Nil()
	return rd
}

// TestRegistrationOverRPC covers an ingester registering a kind and a runner, which is
// the whole ingester facing half of the server.
func TestRegistrationOverRPC(t *testing.T) {
	h := newHarness(t)
	sess := h.dial(t, nil)
	ctx := context.Background()

	if err := sess.Call(ctx, dynamic.MethodRegisterKinds, dynamic.RegisterKindsRequest{
		ID: sess.ID(), Class: `test`, Kinds: []dynamic.RunnerDefinition{proto(t)},
	}, nil); err != nil {
		t.Fatal(err)
	}
	kinds, err := h.store.Kinds()
	if err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 1 {
		t.Fatalf("stored %d kinds, want 1", len(kinds))
	}
	if kinds[0].Kind != `testplugin` {
		t.Errorf("kind = %q", kinds[0].Kind)
	}
	if kinds[0].Name != `` || kinds[0].UUID != uuid.Nil() {
		t.Errorf("a registration should carry no identity, got %+v", kinds[0])
	}
	// the unexported member is not config and the secret carries no value
	for _, v := range kinds[0].Variables {
		if v.Name == `derived` {
			t.Error(`an unexported member was registered`)
		}
	}

	// registering again is an update, not a duplicate, an ingester restart says the same
	// thing twice
	if err = sess.Call(ctx, dynamic.MethodRegisterKinds, dynamic.RegisterKindsRequest{
		ID: sess.ID(), Kinds: []dynamic.RunnerDefinition{proto(t)},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if kinds, err = h.store.Kinds(); err != nil {
		t.Fatal(err)
	} else if len(kinds) != 1 {
		t.Errorf("re-registering produced %d rows, want 1", len(kinds))
	}

	// now a configured runner
	rd, err := dynamic.MapRunnerDefinition(`testplugin`, `prod`, pluginConfig{
		Tag_Name: `test`, Page_Size: 100, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rd.UUID = uuid.New()
	if err = h.store.PutRunner(rd); err != nil {
		t.Fatal(err)
	}
	runners, err := h.store.Runners()
	if err != nil {
		t.Fatal(err)
	}
	if len(runners) != 1 || runners[0].Name != `prod` {
		t.Fatalf("stored runners = %+v", runners)
	}

	// and the ingester can read back what it should be running
	var set dynamic.RunnerSet
	if err = sess.Call(ctx, dynamic.MethodListRunners, dynamic.RunnerQuery{
		ID: sess.ID(), Class: `test`, Kinds: []string{`testplugin`},
	}, &set); err != nil {
		t.Fatal(err)
	}
	if len(set.Runners) != 1 || set.Runners[0].UUID != rd.UUID {
		t.Errorf("listRunners returned %+v", set.Runners)
	}

	// a query that cannot run the kind gets nothing
	var none dynamic.RunnerSet
	if err = sess.Call(ctx, dynamic.MethodListRunners, dynamic.RunnerQuery{
		ID: sess.ID(), Kinds: []string{`somethingelse`},
	}, &none); err != nil {
		t.Fatal(err)
	}
	if len(none.Runners) != 0 {
		t.Errorf("an ingester that cannot run the kind was handed %+v", none.Runners)
	}
}

// TestUIFlow walks the interface the way a person does: see the registered kind, open its
// form, create a runner, see it listed, open it again prefilled, change it and save.
func TestUIFlow(t *testing.T) {
	h := newHarness(t)
	sess := h.dial(t, nil)
	if err := sess.Call(context.Background(), dynamic.MethodRegisterKinds, dynamic.RegisterKindsRequest{
		ID: sess.ID(), Kinds: []dynamic.RunnerDefinition{proto(t)},
	}, nil); err != nil {
		t.Fatal(err)
	}

	// the page itself renders
	if code, body := h.get(t, `/`); code != http.StatusOK {
		t.Fatalf("GET / = %d", code)
	} else if !strings.Contains(body, `hx-get="/ui/kinds"`) {
		t.Error(`the page does not wire up the kinds panel`)
	}

	// the registered kind is offered
	code, body := h.get(t, `/ui/kinds`)
	if code != http.StatusOK {
		t.Fatalf("GET /ui/kinds = %d", code)
	}
	if !strings.Contains(body, `testplugin`) {
		t.Fatalf("the kind is not listed: %s", body)
	}
	if !strings.Contains(body, `/ui/new?kind=testplugin`) {
		t.Error(`clicking the kind does not open a form`)
	}

	// nothing is configured yet
	if _, body = h.get(t, `/ui/runners`); !strings.Contains(body, `Nothing configured`) {
		t.Errorf("runners panel = %s", body)
	}

	// the form for a new runner draws a control for every variable
	code, body = h.get(t, `/ui/new?kind=testplugin`)
	if code != http.StatusOK {
		t.Fatalf("GET /ui/new = %d", code)
	}
	for _, want := range []string{
		`name="var.Tag-Name"`,       // string
		`name="var.Page-Size"`,      // int
		`name="var.Rate"`,           // float
		`name="var.Enabled"`,        // bool
		`name="var.Sections"`,       // []string
		`type="checkbox"`,           // the bool control
		`type="number"`,             // the numeric control
		`id="list-Sections"`,        // the repeating list control
		`data-remove=".listrow"`,    // with a remove button per row
		`/ui/listrow?name=Sections`, // and an add button
		`Create runner`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the new runner form is missing %q", want)
		}
	}
	id := formValue(t, body, `uuid`)
	if _, err := uuid.Parse(id); err != nil {
		t.Fatalf("the form did not carry a usable uuid: %q", id)
	}

	// create it
	form := url.Values{}
	form.Set(`kind`, `testplugin`)
	form.Set(`uuid`, id)
	form.Set(`name`, `prod`)
	form.Set(`var.Tag-Name`, `testtag`)
	form.Set(`var.Page-Size`, `250`)
	form.Set(`var.Rate`, `1.5`)
	form.Set(`var.Enabled`, `true`)
	form[`var.Sections`] = []string{`GENERAL`, `HARDWARE`} // one input per entry
	form.Set(`var.Token`, `hunter2`)
	code, body, hdr := h.post(t, `/ui/save`, form)
	if code != http.StatusOK {
		t.Fatalf("POST /ui/save = %d: %s", code, body)
	}
	if !strings.Contains(body, `Saved`) {
		t.Errorf("save did not confirm: %s", body)
	}
	if hdr.Get(`X-Refresh`) == `` {
		t.Error(`a save should tell the page to refresh its lists`)
	}

	// it is stored, with the values converted to their declared types rather than left
	// as the strings the browser sent
	stored, err := h.store.Runner(uuid.MustParse(id))
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != `prod` || stored.Kind != `testplugin` {
		t.Fatalf("stored %+v", stored)
	}
	// the values are compared by meaning rather than by Go type.  Storage is JSON, so a
	// number comes back as a float64 and a []string as a []any of strings no matter what
	// went in, which is also exactly what an ingester receives over the wire.
	assertValue(t, stored, `Tag-Name`, `testtag`)
	assertValue(t, stored, `Page-Size`, `250`)
	assertValue(t, stored, `Rate`, `1.5`)
	assertValue(t, stored, `Enabled`, `true`)
	assertValue(t, stored, `Sections`, `[GENERAL HARDWARE]`)

	// and the stored definition is still something the ingester can turn into a config,
	// which is the only thing the types really have to support
	ini, err := stored.INI()
	if err != nil {
		t.Fatalf("the stored definition cannot be rendered: %v", err)
	}
	for _, want := range []string{`[testplugin "prod"]`, "Tag-Name=`testtag`", `Page-Size=250`, `Enabled=true`} {
		if !strings.Contains(ini, want) {
			t.Errorf("the rendered config is missing %q:\n%s", want, ini)
		}
	}

	// it shows up in the list and can be opened again, prefilled
	if _, body = h.get(t, `/ui/runners`); !strings.Contains(body, `prod`) {
		t.Errorf("the runner is not listed: %s", body)
	}
	code, body = h.get(t, `/ui/edit?uuid=`+id)
	if code != http.StatusOK {
		t.Fatalf("GET /ui/edit = %d", code)
	}
	for _, want := range []string{
		`value="prod"`, `value="testtag"`, `value="250"`, `value="1.5"`,
		`checked`, `GENERAL`, `HARDWARE`, `Save changes`, `Delete`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the edit form is missing %q", want)
		}
	}

	// change it and save again, same UUID so it updates in place
	form.Set(`var.Page-Size`, `500`)
	form.Del(`var.Enabled`) // an unchecked box sends nothing
	if code, body, _ = h.post(t, `/ui/save`, form); code != http.StatusOK {
		t.Fatalf("POST /ui/save = %d: %s", code, body)
	}
	if stored, err = h.store.Runner(uuid.MustParse(id)); err != nil {
		t.Fatal(err)
	}
	assertValue(t, stored, `Page-Size`, `500`)
	assertValue(t, stored, `Enabled`, `false`)
	if runners, _ := h.store.Runners(); len(runners) != 1 {
		t.Errorf("editing produced %d runners, want 1 updated in place", len(runners))
	}

	// and delete
	if code, body, hdr = h.post(t, `/ui/delete?uuid=`+id, nil); code != http.StatusOK {
		t.Fatalf("POST /ui/delete = %d: %s", code, body)
	}
	if hdr.Get(`X-Refresh`) == `` {
		t.Error(`a delete should refresh the lists`)
	}
	if runners, _ := h.store.Runners(); len(runners) != 0 {
		t.Errorf("%d runners survived the delete", len(runners))
	}
}

// TestUIPushesToIngester is the point of the whole exercise: saving in the browser has to
// reach a running ingester.
func TestUIPushesToIngester(t *testing.T) {
	h := newHarness(t)

	var mtx sync.Mutex
	var applied []dynamic.RunnerDefinition
	mux := rpc.NewMux()
	if err := mux.Register(dynamic.MethodApplyConfig, func(_ context.Context, params json.RawMessage) (any, error) {
		var rd dynamic.RunnerDefinition
		if err := json.Unmarshal(params, &rd); err != nil {
			return nil, err
		}
		mtx.Lock()
		applied = append(applied, rd)
		mtx.Unlock()
		return map[string]any{`ok`: true}, nil
	}); err != nil {
		t.Fatal(err)
	}

	sess := h.dial(t, mux)
	if err := sess.Call(context.Background(), dynamic.MethodRegisterKinds, dynamic.RegisterKindsRequest{
		ID: sess.ID(), Kinds: []dynamic.RunnerDefinition{proto(t)},
	}, nil); err != nil {
		t.Fatal(err)
	}
	// the page should say an ingester is connected
	if _, body := h.get(t, `/ui/status`); !strings.Contains(body, `1 ingester`) {
		t.Errorf("status = %s", body)
	}

	_, body := h.get(t, `/ui/new?kind=testplugin`)
	id := formValue(t, body, `uuid`)

	form := url.Values{}
	form.Set(`kind`, `testplugin`)
	form.Set(`uuid`, id)
	form.Set(`name`, `pushed`)
	form.Set(`var.Tag-Name`, `pushtag`)
	code, body, _ := h.post(t, `/ui/save`, form)
	if code != http.StatusOK {
		t.Fatalf("save = %d: %s", code, body)
	}
	if !strings.Contains(body, `pushed to 1`) {
		t.Errorf("the save did not report a push: %s", body)
	}

	mtx.Lock()
	defer mtx.Unlock()
	if len(applied) != 1 {
		t.Fatalf("the ingester received %d configs, want 1", len(applied))
	}
	if applied[0].Name != `pushed` || applied[0].Kind != `testplugin` {
		t.Errorf("the ingester received %+v", applied[0])
	}
	if applied[0].UUID.String() != id {
		t.Errorf("the pushed config has UUID %v, want %s", applied[0].UUID, id)
	}
}

// TestUIRejectsBadInput covers the guards on the form handler.
func TestUIRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	sess := h.dial(t, nil)
	if err := sess.Call(context.Background(), dynamic.MethodRegisterKinds, dynamic.RegisterKindsRequest{
		ID: sess.ID(), Kinds: []dynamic.RunnerDefinition{proto(t)},
	}, nil); err != nil {
		t.Fatal(err)
	}
	_, body := h.get(t, `/ui/new?kind=testplugin`)
	id := formValue(t, body, `uuid`)

	base := func() url.Values {
		f := url.Values{}
		f.Set(`kind`, `testplugin`)
		f.Set(`uuid`, id)
		f.Set(`name`, `prod`)
		return f
	}

	for _, tc := range []struct {
		name string
		form url.Values
		want string
	}{
		{`no name`, func() url.Values { f := base(); f.Set(`name`, ``); return f }(), `name`},
		{`unknown kind`, func() url.Values { f := base(); f.Set(`kind`, `nope`); return f }(), `not found`},
		{`bad uuid`, func() url.Values { f := base(); f.Set(`uuid`, `not-a-uuid`); return f }(), `invalid`},
		{`int that is not a number`, func() url.Values { f := base(); f.Set(`var.Page-Size`, `many`); return f }(), `Page-Size`},
		{`float that is not a number`, func() url.Values { f := base(); f.Set(`var.Rate`, `fast`); return f }(), `Rate`},
	} {
		code, body, _ := h.post(t, `/ui/save`, tc.form)
		if code != http.StatusOK {
			t.Errorf("%s: status %d, the UI should render the error rather than fail the request", tc.name, code)
		}
		if !strings.Contains(strings.ToLower(body), strings.ToLower(tc.want)) {
			t.Errorf("%s: response %q does not mention %q", tc.name, body, tc.want)
		}
		if runners, _ := h.store.Runners(); len(runners) != 0 {
			t.Fatalf("%s: a rejected save stored something", tc.name)
		}
	}

	// a field the prototype never declared is ignored rather than smuggled in
	f := base()
	f.Set(`var.Not-A-Real-Setting`, `whatever`)
	if code, body, _ := h.post(t, `/ui/save`, f); code != http.StatusOK {
		t.Fatalf("save = %d: %s", code, body)
	}
	stored, err := h.store.Runner(uuid.MustParse(id))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range stored.Variables {
		if v.Name == `Not-A-Real-Setting` {
			t.Error(`a variable the ingester never advertised was accepted from the browser`)
		}
	}
}

// TestRPCRequiresTheToken checks that the RPC route is actually protected.
func TestRPCRequiresTheToken(t *testing.T) {
	h := newHarness(t)
	ctx, cf := context.WithTimeout(context.Background(), 10*time.Second)
	defer cf()
	if s, err := rpc.Dial(ctx, rpc.ClientConfig{
		Webserver: h.ts.URL, Path: client.INGESTERS_CONTROL_URL, Token: `the-wrong-secret`, PingInterval: -1,
	}); err == nil {
		s.Close()
		t.Fatal(`an ingester with the wrong secret connected`)
	}
}

// formValue pulls a hidden input's value out of a rendered form.
func formValue(t *testing.T, body, name string) string {
	t.Helper()
	marker := `name="` + name + `" value="`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("no %s field in %s", name, body)
	}
	rest := body[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatalf("unterminated %s field", name)
	}
	return rest[:j]
}

// assertValue checks a stored variable by its rendered value, which is stable across the
// JSON round trip that storage and the wire both impose.
func assertValue(t *testing.T, rd dynamic.RunnerDefinition, name, want string) {
	t.Helper()
	for _, v := range rd.Variables {
		if v.Name != name {
			continue
		}
		if got := fmt.Sprintf("%v", v.Value); got != want {
			t.Errorf("%s = %v (%T), want %s", name, v.Value, v.Value, want)
		}
		return
	}
	t.Errorf("%s is not in the stored definition", name)
}

// TestAssignmentUI covers the two pickers: what they offer, what a save records, and what
// comes back prefilled.
func TestAssignmentUI(t *testing.T) {
	h := newHarness(t)

	// two ingesters that can run testplugin and one that cannot
	edge := h.dialAs(t, `edge`)
	core := h.dialAs(t, `core`)
	other := h.dialAs(t, `dmz`)
	reg := func(sess *rpc.Session, kinds ...dynamic.RunnerDefinition) {
		t.Helper()
		if err := sess.Call(context.Background(), dynamic.MethodRegisterKinds, dynamic.RegisterKindsRequest{
			ID: sess.ID(), Kinds: kinds,
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	otherProto, err := dynamic.MapRunnerDefinition(`otherplugin`, `otherplugin`, pluginConfig{})
	if err != nil {
		t.Fatal(err)
	}
	otherProto.Name, otherProto.UUID = ``, uuid.Nil()
	reg(edge, proto(t))
	reg(core, proto(t))
	reg(other, otherProto)

	// the picker offers only the ingesters that registered this kind
	_, body := h.get(t, `/ui/new?kind=testplugin`)
	if !strings.Contains(body, edge.ID().String()) || !strings.Contains(body, core.ID().String()) {
		t.Errorf("the picker is missing an ingester that registered the kind:\n%s", body)
	}
	if strings.Contains(body, other.ID().String()) {
		t.Error(`the picker offered an ingester that cannot run this kind`)
	}
	// and the classes that have been seen
	for _, want := range []string{`assign.uuid`, `assign.class`, `assign.otherclasses`, `>edge<`, `>core<`} {
		if !strings.Contains(body, want) {
			t.Errorf("the assignment picker is missing %q", want)
		}
	}

	// save with both filters set
	id := formValue(t, body, `uuid`)
	form := url.Values{}
	form.Set(`kind`, `testplugin`)
	form.Set(`uuid`, id)
	form.Set(`name`, `pinned`)
	form.Set(`var.Tag-Name`, `t`)
	form[`assign.uuid`] = []string{edge.ID().String()}
	form[`assign.class`] = []string{`edge`, `core`}
	form.Set(`assign.otherclasses`, `lab, staging`)
	if code, b, _ := h.post(t, `/ui/save`, form); code != http.StatusOK {
		t.Fatalf("save = %d: %s", code, b)
	}

	stored, err := h.store.Runner(uuid.MustParse(id))
	if err != nil {
		t.Fatal(err)
	}
	if stored.Assigned == nil {
		t.Fatal(`the assignment was not stored`)
	}
	if len(stored.Assigned.UUIDs) != 1 || stored.Assigned.UUIDs[0] != edge.ID() {
		t.Errorf("UUIDs = %v, want just %v", stored.Assigned.UUIDs, edge.ID())
	}
	wantClasses := []string{`core`, `edge`, `lab`, `staging`}
	if !reflect.DeepEqual(stored.Assigned.Classes, wantClasses) {
		t.Errorf("Classes = %v, want %v", stored.Assigned.Classes, wantClasses)
	}

	// reopening shows the selections
	_, body = h.get(t, `/ui/edit?uuid=`+id)
	if !strings.Contains(body, `value="`+edge.ID().String()+`" selected`) {
		t.Error(`the pinned ingester is not preselected`)
	}
	if !strings.Contains(body, `value="edge" selected`) {
		t.Error(`the pinned class is not preselected`)
	}
	// classes nothing is carrying come back in the free text box rather than vanishing
	if !strings.Contains(body, `value="lab, staging"`) {
		t.Errorf("the hand typed classes were lost:\n%s", body)
	}

	// clearing both makes it unassigned again
	form[`assign.uuid`] = nil
	form[`assign.class`] = nil
	form.Set(`assign.otherclasses`, ``)
	if code, b, _ := h.post(t, `/ui/save`, form); code != http.StatusOK {
		t.Fatalf("save = %d: %s", code, b)
	}
	if stored, err = h.store.Runner(uuid.MustParse(id)); err != nil {
		t.Fatal(err)
	}
	if !stored.Assigned.Empty() {
		t.Errorf("clearing the pickers left %+v", stored.Assigned)
	}

	// a UUID that does not parse is refused rather than dropped, silently widening who
	// receives a config is the wrong way to fail
	form[`assign.uuid`] = []string{`not-a-uuid`}
	code, b, _ := h.post(t, `/ui/save`, form)
	if code != http.StatusOK {
		t.Fatalf("save = %d", code)
	}
	if !strings.Contains(b, `invalid assignment UUID`) {
		t.Errorf("a bad assignment UUID was accepted: %s", b)
	}
}

// TestIngestersDropdown covers the panel behind the connection chip.
func TestIngestersDropdown(t *testing.T) {
	h := newHarness(t)
	sess := h.dialAs(t, `edge`)
	if err := sess.Call(context.Background(), dynamic.MethodRegisterKinds, dynamic.RegisterKindsRequest{
		ID: sess.ID(), Kinds: []dynamic.RunnerDefinition{proto(t)},
	}, nil); err != nil {
		t.Fatal(err)
	}

	// the chip opens the panel
	_, body := h.get(t, `/ui/status`)
	if !strings.Contains(body, `data-toggle="#ingesters"`) || !strings.Contains(body, `hx-get="/ui/ingesters"`) {
		t.Errorf("the status chip does not open the ingester list:\n%s", body)
	}
	if !strings.Contains(body, `1 ingester`) {
		t.Errorf("status = %s", body)
	}

	// the panel shows the UUID, class and kinds
	code, body := h.get(t, `/ui/ingesters`)
	if code != http.StatusOK {
		t.Fatalf("GET /ui/ingesters = %d", code)
	}
	for _, want := range []string{sess.ID().String(), `edge`, `testplugin`, `dot on`} {
		if !strings.Contains(body, want) {
			t.Errorf("the ingester list is missing %q:\n%s", want, body)
		}
	}

	// a disconnected ingester is still listed, just not marked connected
	sess.Close()
	waitForCond(t, `the session to be dropped`, func() bool {
		_, b := h.get(t, `/ui/ingesters`)
		return strings.Contains(b, `dot off`)
	})
	_, body = h.get(t, `/ui/ingesters`)
	if !strings.Contains(body, sess.ID().String()) {
		t.Error(`a disconnected ingester disappeared from the list entirely`)
	}
}

// TestPollingRespectsAssignment is the server side of the filter: a client polling must
// never be handed a configuration pinned to somebody else.
func TestPollingRespectsAssignment(t *testing.T) {
	h := newHarness(t)
	edge := h.dialAs(t, `edge`)
	core := h.dialAs(t, `core`)
	for _, sess := range []*rpc.Session{edge, core} {
		if err := sess.Call(context.Background(), dynamic.MethodRegisterKinds, dynamic.RegisterKindsRequest{
			ID: sess.ID(), Kinds: []dynamic.RunnerDefinition{proto(t)},
		}, nil); err != nil {
			t.Fatal(err)
		}
	}

	put := func(name string, a *dynamic.Assignment) uuid.UUID {
		t.Helper()
		rd, err := dynamic.MapRunnerDefinition(`testplugin`, name, pluginConfig{Tag_Name: name})
		if err != nil {
			t.Fatal(err)
		}
		rd.UUID = uuid.New()
		rd.Assigned = a
		if err = h.store.PutRunner(rd); err != nil {
			t.Fatal(err)
		}
		return rd.UUID
	}

	unassigned := put(`everyone`, nil)
	toEdgeID := put(`edgeonly`, &dynamic.Assignment{UUIDs: []uuid.UUID{edge.ID()}})
	toBoth := put(`both`, &dynamic.Assignment{UUIDs: []uuid.UUID{edge.ID(), core.ID()}})
	toEdgeClass := put(`edgeclass`, &dynamic.Assignment{Classes: []string{`edge`}})
	toNeither := put(`nobody`, &dynamic.Assignment{UUIDs: []uuid.UUID{uuid.New()}})

	poll := func(sess *rpc.Session) map[uuid.UUID]bool {
		t.Helper()
		var set dynamic.RunnerSet
		// the body deliberately lies about identity, the server must use the session
		if err := sess.Call(context.Background(), dynamic.MethodListRunners, dynamic.RunnerQuery{
			ID: uuid.New(), Class: `impersonated`, Kinds: []string{`testplugin`},
		}, &set); err != nil {
			t.Fatal(err)
		}
		got := map[uuid.UUID]bool{}
		for _, rd := range set.Runners {
			got[rd.UUID] = true
		}
		return got
	}

	edgeGot := poll(edge)
	coreGot := poll(core)

	for _, tc := range []struct {
		name string
		id   uuid.UUID
		edge bool
		core bool
	}{
		{`unassigned goes to both`, unassigned, true, true},
		{`pinned to edge`, toEdgeID, true, false},
		{`pinned to both`, toBoth, true, true},
		{`pinned to the edge class`, toEdgeClass, true, false},
		{`pinned to a third party`, toNeither, false, false},
	} {
		if edgeGot[tc.id] != tc.edge {
			t.Errorf("%s: edge got it = %v, want %v", tc.name, edgeGot[tc.id], tc.edge)
		}
		if coreGot[tc.id] != tc.core {
			t.Errorf("%s: core got it = %v, want %v", tc.name, coreGot[tc.id], tc.core)
		}
	}
}

// waitForCond polls until cond holds or the deadline passes.
func waitForCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestMain turns the authentication throttle off for the suite.  These tests connect
// several ingesters from 127.0.0.1 in quick succession, which is exactly what the
// throttle exists to refuse, and the throttle has its own tests in the rpc package.
func TestMain(m *testing.M) {
	authRateWindow = -1
	os.Exit(m.Run())
}

// TestPushRespectsAssignment checks that a save only reaches the ingesters the
// configuration is assigned to.  The poll filters correctly on its own, but a push that
// ignored the assignment would deliver a pinned configuration to everybody the moment it
// was saved, which would make the pinning cosmetic.
func TestPushRespectsAssignment(t *testing.T) {
	h := newHarness(t)

	type spy struct {
		sess *rpc.Session
		mtx  sync.Mutex
		got  []string
	}
	newSpy := func(class string, kinds ...dynamic.RunnerDefinition) *spy {
		t.Helper()
		sp := &spy{}
		mux := rpc.NewMux()
		if err := mux.Register(dynamic.MethodApplyConfig, func(_ context.Context, params json.RawMessage) (any, error) {
			var rd dynamic.RunnerDefinition
			if err := json.Unmarshal(params, &rd); err != nil {
				return nil, err
			}
			sp.mtx.Lock()
			sp.got = append(sp.got, rd.Name)
			sp.mtx.Unlock()
			return map[string]any{`ok`: true}, nil
		}); err != nil {
			t.Fatal(err)
		}
		ctx, cf := context.WithTimeout(context.Background(), 10*time.Second)
		defer cf()
		s, err := rpc.Dial(ctx, rpc.ClientConfig{
			Webserver: h.ts.URL, Path: client.INGESTERS_CONTROL_URL, Token: testSecret,
			ID: uuid.New(), Class: class, Handlers: mux, PingInterval: -1,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		sp.sess = s
		if err = s.Call(context.Background(), dynamic.MethodRegisterKinds, dynamic.RegisterKindsRequest{
			ID: s.ID(), Kinds: kinds,
		}, nil); err != nil {
			t.Fatal(err)
		}
		return sp
	}
	saw := func(sp *spy) []string {
		sp.mtx.Lock()
		defer sp.mtx.Unlock()
		return append([]string{}, sp.got...)
	}

	otherProto, err := dynamic.MapRunnerDefinition(`otherplugin`, `otherplugin`, pluginConfig{})
	if err != nil {
		t.Fatal(err)
	}
	otherProto.Name, otherProto.UUID = ``, uuid.Nil()

	edge := newSpy(`edge`, proto(t))
	core := newSpy(`core`, proto(t))
	wrongKind := newSpy(`edge`, otherProto)

	save := func(name string, assign url.Values) {
		t.Helper()
		_, body := h.get(t, `/ui/new?kind=testplugin`)
		form := url.Values{}
		form.Set(`kind`, `testplugin`)
		form.Set(`uuid`, formValue(t, body, `uuid`))
		form.Set(`name`, name)
		form.Set(`var.Tag-Name`, name)
		maps.Copy(form, assign)
		if code, b, _ := h.post(t, `/ui/save`, form); code != http.StatusOK {
			t.Fatalf("save %s = %d: %s", name, code, b)
		}
	}

	save(`everyone`, nil)
	save(`edgeonly`, url.Values{`assign.uuid`: {edge.sess.ID().String()}})
	save(`edgeclass`, url.Values{`assign.class`: {`edge`}})
	save(`nobody`, url.Values{`assign.uuid`: {uuid.New().String()}})

	// an unassigned config reaches both ingesters that can run the kind, a pinned one
	// reaches only its target, and neither reaches the one that cannot run the kind
	if got, want := saw(edge), []string{`everyone`, `edgeonly`, `edgeclass`}; !reflect.DeepEqual(got, want) {
		t.Errorf("edge received %v, want %v", got, want)
	}
	if got, want := saw(core), []string{`everyone`}; !reflect.DeepEqual(got, want) {
		t.Errorf("core received %v, want %v", got, want)
	}
	if got := saw(wrongKind); len(got) != 0 {
		t.Errorf("an ingester that cannot run the kind received %v", got)
	}
}

// TestSecretsInTheUI covers how a secret is presented and saved: masked, never rendered
// back, and left alone when the box is submitted empty.
func TestSecretsInTheUI(t *testing.T) {
	h := newHarness(t)
	sess := h.dialAs(t, `edge`)
	if err := sess.Call(context.Background(), dynamic.MethodRegisterKinds, dynamic.RegisterKindsRequest{
		ID: sess.ID(), Kinds: []dynamic.RunnerDefinition{proto(t)},
	}, nil); err != nil {
		t.Fatal(err)
	}

	// the registration types it as a secret rather than a string
	stored, err := h.store.Kind(`testplugin`)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, v := range stored.Variables {
		if v.Name == `Token` {
			found = true
			if string(v.Type) != `secret` {
				t.Errorf("Token is typed %s, want secret", v.Type)
			}
		}
	}
	if !found {
		t.Fatal(`Token is not in the registration at all`)
	}

	// a new form draws a masked input, not a text one
	_, body := h.get(t, `/ui/new?kind=testplugin`)
	if !strings.Contains(body, `type="password" name="var.Token"`) {
		t.Errorf("the secret is not a password input:\n%s", body)
	}
	// the badge says whether anything is stored, so "empty" and "hidden" are distinct
	if !strings.Contains(body, `>none stored<`) {
		t.Errorf("an unset secret should be badged empty:\n%s", body)
	}
	id := formValue(t, body, `uuid`)

	// save one with a value
	const secret = `SUPER-SECRET-TOKEN-VALUE`
	form := url.Values{}
	form.Set(`kind`, `testplugin`)
	form.Set(`uuid`, id)
	form.Set(`name`, `prod`)
	form.Set(`var.Tag-Name`, `t`)
	form.Set(`var.Token`, secret)
	if code, b, _ := h.post(t, `/ui/save`, form); code != http.StatusOK {
		t.Fatalf("save = %d: %s", code, b)
	}
	run, err := h.store.Runner(uuid.MustParse(id))
	if err != nil {
		t.Fatal(err)
	}
	if got := valueOf(run, `Token`); got != secret {
		t.Errorf("the secret was stored as %v, want %q", got, secret)
	}

	// reopening must not put it back in the page.  A password input still carries its
	// value in the page source, so the value simply never leaves the server.
	_, body = h.get(t, `/ui/edit?uuid=`+id)
	if strings.Contains(body, secret) {
		t.Errorf("the secret was rendered back into the form:\n%s", body)
	}
	if !strings.Contains(body, `type="password" name="var.Token"`) {
		t.Error(`the edit form does not mask the secret`)
	}
	if !strings.Contains(body, `>stored<`) {
		t.Error(`a stored secret should be badged stored`)
	}
	if !strings.Contains(body, `leave blank to keep the stored value`) {
		t.Error(`the form does not say an empty box keeps the stored value`)
	}

	// saving with the box left empty keeps what is stored rather than wiping it, which is
	// the only way editing anything else on the form can work
	form.Set(`var.Token`, ``)
	form.Set(`var.Tag-Name`, `changed`)
	if code, b, _ := h.post(t, `/ui/save`, form); code != http.StatusOK {
		t.Fatalf("save = %d: %s", code, b)
	}
	if run, err = h.store.Runner(uuid.MustParse(id)); err != nil {
		t.Fatal(err)
	}
	if got := valueOf(run, `Token`); got != secret {
		t.Errorf("an empty box wiped the secret, Token = %v", got)
	}
	if got := valueOf(run, `Tag-Name`); got != `changed` {
		t.Errorf("the rest of the form did not save, Tag-Name = %v", got)
	}

	// and a new value replaces it
	form.Set(`var.Token`, `A-DIFFERENT-SECRET`)
	if code, b, _ := h.post(t, `/ui/save`, form); code != http.StatusOK {
		t.Fatalf("save = %d: %s", code, b)
	}
	if run, err = h.store.Runner(uuid.MustParse(id)); err != nil {
		t.Fatal(err)
	}
	if got := valueOf(run, `Token`); got != `A-DIFFERENT-SECRET` {
		t.Errorf("the secret was not replaced, Token = %v", got)
	}

	// the config written for the ingester still carries it, a secret the ingester never
	// receives is a secret that does not work
	ini, err := run.INI()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ini, `A-DIFFERENT-SECRET`) {
		t.Errorf("the secret is missing from the generated config:\n%s", ini)
	}
}

// valueOf pulls a variable's value out of a definition.
func valueOf(rd dynamic.RunnerDefinition, name string) any {
	for _, v := range rd.Variables {
		if v.Name == name {
			return v.Value
		}
	}
	return nil
}

// TestListControl covers the repeating input a []string is drawn with.
func TestListControl(t *testing.T) {
	h := newHarness(t)
	sess := h.dialAs(t, `edge`)
	if err := sess.Call(context.Background(), dynamic.MethodRegisterKinds, dynamic.RegisterKindsRequest{
		ID: sess.ID(), Kinds: []dynamic.RunnerDefinition{proto(t)},
	}, nil); err != nil {
		t.Fatal(err)
	}

	// a fresh form draws one empty row so there is somewhere to type, and no textarea
	_, body := h.get(t, `/ui/new?kind=testplugin`)
	if strings.Contains(body, `<textarea`) {
		t.Error(`a []string should not be a textarea any more`)
	}
	if n := strings.Count(body, `name="var.Sections"`); n != 1 {
		t.Errorf("a new form drew %d rows for Sections, want 1", n)
	}
	id := formValue(t, body, `uuid`)

	// the add button asks the server for another row
	code, row := h.get(t, `/ui/listrow?name=Sections`)
	if code != http.StatusOK {
		t.Fatalf("GET /ui/listrow = %d", code)
	}
	if !strings.Contains(row, `name="var.Sections"`) || !strings.Contains(row, `data-remove=".listrow"`) {
		t.Errorf("the added row is not usable: %s", row)
	}
	// the name is echoed into the field, so it has to be escaped
	_, evil := h.get(t, `/ui/listrow?name=`+url.QueryEscape(`"><script>alert(1)</script>`))
	if strings.Contains(evil, `<script>`) {
		t.Errorf("the row name is not escaped: %s", evil)
	}

	// several rows post under the same name and come back as a slice
	form := url.Values{}
	form.Set(`kind`, `testplugin`)
	form.Set(`uuid`, id)
	form.Set(`name`, `prod`)
	form.Set(`var.Tag-Name`, `t`)
	form[`var.Sections`] = []string{`GENERAL`, `HARDWARE`, `STORAGE`}
	if c, b, _ := h.post(t, `/ui/save`, form); c != http.StatusOK {
		t.Fatalf("save = %d: %s", c, b)
	}
	stored, err := h.store.Runner(uuid.MustParse(id))
	if err != nil {
		t.Fatal(err)
	}
	if got := listOf(t, stored, `Sections`); !reflect.DeepEqual(got, []string{`GENERAL`, `HARDWARE`, `STORAGE`}) {
		t.Errorf("Sections = %v", got)
	}

	// reopening draws one row per stored entry
	_, body = h.get(t, `/ui/edit?uuid=`+id)
	if n := strings.Count(body, `name="var.Sections"`); n != 3 {
		t.Errorf("the edit form drew %d rows, want 3", n)
	}
	for _, want := range []string{`value="GENERAL"`, `value="HARDWARE"`, `value="STORAGE"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the edit form is missing %q", want)
		}
	}

	// removing rows in the browser just posts fewer of them, and a blank row left behind
	// is dropped rather than stored as an empty entry
	form[`var.Sections`] = []string{`GENERAL`, ``, `  `, `STORAGE`}
	if c, b, _ := h.post(t, `/ui/save`, form); c != http.StatusOK {
		t.Fatalf("save = %d: %s", c, b)
	}
	if stored, err = h.store.Runner(uuid.MustParse(id)); err != nil {
		t.Fatal(err)
	}
	if got := listOf(t, stored, `Sections`); !reflect.DeepEqual(got, []string{`GENERAL`, `STORAGE`}) {
		t.Errorf("after removing a row Sections = %v, want [GENERAL STORAGE]", got)
	}

	// removing every row leaves the variable unset rather than an empty list
	form[`var.Sections`] = []string{``}
	if c, b, _ := h.post(t, `/ui/save`, form); c != http.StatusOK {
		t.Fatalf("save = %d: %s", c, b)
	}
	if stored, err = h.store.Runner(uuid.MustParse(id)); err != nil {
		t.Fatal(err)
	}
	if v := valueOf(stored, `Sections`); v != nil {
		t.Errorf("clearing every row left %v (%T), want nothing", v, v)
	}
	// and the generated config simply omits it
	ini, err := stored.INI()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ini, `Sections`) {
		t.Errorf("an unset list should not be written:\n%s", ini)
	}
}

// listOf reads a []string variable back out of a stored definition.  Storage is JSON, so
// what went in as a []string comes back as a []any of strings, exactly as it does over
// the wire to an ingester.
func listOf(t *testing.T, rd dynamic.RunnerDefinition, name string) []string {
	t.Helper()
	switch v := valueOf(rd, name).(type) {
	case nil:
		return nil
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				t.Fatalf("%s holds a %T, want strings", name, e)
			}
			out = append(out, s)
		}
		return out
	default:
		t.Fatalf("%s is a %T, want a list", name, v)
	}
	return nil
}

// TestRequiredInTheUI covers how a required member is drawn and enforced.
func TestRequiredInTheUI(t *testing.T) {
	h := newHarness(t)
	sess := h.dialAs(t, `edge`)
	rp, err := dynamic.MapRunnerDefinition(`reqplugin`, `reqplugin`, requiredPluginConfig{})
	if err != nil {
		t.Fatal(err)
	}
	rp.Name, rp.UUID = ``, uuid.Nil()
	if err := sess.Call(context.Background(), dynamic.MethodRegisterKinds, dynamic.RegisterKindsRequest{
		ID: sess.ID(), Kinds: []dynamic.RunnerDefinition{rp},
	}, nil); err != nil {
		t.Fatal(err)
	}

	_, body := h.get(t, `/ui/new?kind=reqplugin`)
	// a required plain member gets the browser's own enforcement and the marker
	if !strings.Contains(body, `name="var.Tag-Name" value="" required`) {
		t.Errorf("a required member is not marked required:\n%s", body)
	}
	// a required secret with nothing stored is required too
	if !strings.Contains(body, `required `) || !strings.Contains(body, `type="password" name="var.Token"`) {
		t.Error(`a required secret on a new config should be required`)
	}
	// an optional one is not
	if strings.Contains(body, `name="var.Page-Size" value="" required`) {
		t.Error(`an optional member was marked required`)
	}
	id := formValue(t, body, `uuid`)

	// the server refuses a missing required member even though the browser would have
	// caught it, a form post is not only ever made by a browser
	form := url.Values{}
	form.Set(`kind`, `reqplugin`)
	form.Set(`uuid`, id)
	form.Set(`name`, `prod`)
	form.Set(`var.Token`, `a-secret`)
	if _, b, _ := h.post(t, `/ui/save`, form); !strings.Contains(b, `Tag-Name is required`) {
		t.Errorf("a missing required member was accepted: %s", b)
	}
	if rs, _ := h.store.Runners(); len(rs) != 0 {
		t.Fatal(`a rejected save stored something`)
	}

	// and a missing required secret
	form.Set(`var.Tag-Name`, `t`)
	form.Set(`var.Token`, ``)
	if _, b, _ := h.post(t, `/ui/save`, form); !strings.Contains(b, `Token is required`) {
		t.Errorf("a missing required secret was accepted: %s", b)
	}

	// with everything supplied it saves
	form.Set(`var.Token`, `a-secret`)
	if c, b, _ := h.post(t, `/ui/save`, form); c != http.StatusOK || !strings.Contains(b, `Saved`) {
		t.Fatalf("save = %d: %s", c, b)
	}

	// reopening: the required secret is already set, so it must NOT be marked required.
	// The form never shows it and an empty box means "keep it", so a required attribute
	// would make every other field on the form unsavable.
	_, body = h.get(t, `/ui/edit?uuid=`+id)
	if strings.Contains(body, `name="var.Token" autocomplete="new-password"
				required `) {
		t.Error(`a stored secret is still marked required, the form could never be saved`)
	}
	if !strings.Contains(body, `>stored<`) {
		t.Error(`the stored secret is not reported as set`)
	}
	// and saving with it blank works, keeping the stored value
	form.Set(`var.Token`, ``)
	form.Set(`var.Tag-Name`, `changed`)
	if c, b, _ := h.post(t, `/ui/save`, form); c != http.StatusOK {
		t.Fatalf("editing around a stored secret failed: %d %s", c, b)
	}
	run, rerr := h.store.Runner(uuid.MustParse(id))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if got := valueOf(run, `Token`); got != `a-secret` {
		t.Errorf("the stored secret was lost, Token = %v", got)
	}
	if got := valueOf(run, `Tag-Name`); got != `changed` {
		t.Errorf("the edit did not save, Tag-Name = %v", got)
	}
}

// TestFormOffersEnumsAsPickers covers the control an enum earns.  A free text box for a
// value that must be one of eleven named APIs is a guessing game the operator loses by
// saving and reading the rejection.
func TestFormOffersEnumsAsPickers(t *testing.T) {
	h := newHarness(t)
	proto, err := dynamic.MapRunnerDefinition(`SQS`, `SQS`, sqs.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.store.ReplaceKinds(uuid.New(), `edge`, []dynamic.RunnerDefinition{proto}); err != nil {
		t.Fatal(err)
	}
	code, body := h.get(t, `/ui/new?kind=SQS`)
	if code != http.StatusOK {
		t.Fatalf("GET /ui/new = %d", code)
	}
	// a select, carrying exactly the declared choices
	if !strings.Contains(body, `<select name="var.Credentials-Type"`) {
		t.Errorf("Credentials-Type is not a picker:\n%s", body)
	}
	for _, opt := range []string{`static`, `environment`, `ec2role`} {
		if !strings.Contains(body, `<option value="`+opt+`"`) {
			t.Errorf("the picker does not offer %q", opt)
		}
	}
	// and a conditional requirement is spelled out, because an asterisk cannot say "only
	// when Credentials-Type is static"
	if !strings.Contains(body, `required when Credentials-Type is`) {
		t.Errorf("the conditional requirement is not explained:\n%s", body)
	}
}

// TestFormRefusesAValueOutsideTheEnum is the enforcement behind the picker: the set is
// the rule, not a suggestion, and a hand crafted post is still a post.
func TestFormRefusesAValueOutsideTheEnum(t *testing.T) {
	h := newHarness(t)
	proto, err := dynamic.MapRunnerDefinition(`SQS`, `SQS`, sqs.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.store.ReplaceKinds(uuid.New(), `edge`, []dynamic.RunnerDefinition{proto}); err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	form.Set(`kind`, `SQS`)
	form.Set(`name`, `q`)
	form.Set(`uuid`, uuid.New().String())
	form.Set(`var.Queue-URL`, `https://sqs.us-east-1.amazonaws.com/1/q`)
	form.Set(`var.Region`, `us-east-1`)
	form.Set(`var.Credentials-Type`, `nonsense`)
	form.Set(`var.AKID`, `akid`)
	form.Set(`var.Secret`, `sec`)
	_, body, _ := h.post(t, `/ui/save`, form)
	if !strings.Contains(body, `not a valid Credentials-Type`) {
		t.Errorf("a value outside the enum was accepted:\n%s", body)
	}
	if runners, _ := h.store.Runners(); len(runners) != 0 {
		t.Errorf("it was stored anyway: %d runners", len(runners))
	}
}

// TestFormEnforcesConditionalRequirement is the SQS bug end to end: static credentials
// with no key must be refused, and the two role based modes must not be.
func TestFormEnforcesConditionalRequirement(t *testing.T) {
	h := newHarness(t)
	proto, err := dynamic.MapRunnerDefinition(`SQS`, `SQS`, sqs.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.store.ReplaceKinds(uuid.New(), `edge`, []dynamic.RunnerDefinition{proto}); err != nil {
		t.Fatal(err)
	}
	save := func(mode string, withKey bool) string {
		t.Helper()
		form := url.Values{}
		form.Set(`kind`, `SQS`)
		form.Set(`name`, `q-`+mode+fmt.Sprint(withKey))
		form.Set(`uuid`, uuid.New().String())
		form.Set(`var.Queue-URL`, `https://sqs.us-east-1.amazonaws.com/1/q`)
		form.Set(`var.Region`, `us-east-1`)
		if mode != `` {
			form.Set(`var.Credentials-Type`, mode)
		}
		if withKey {
			form.Set(`var.AKID`, `akid`)
			form.Set(`var.Secret`, `sec`)
		}
		_, body, _ := h.post(t, `/ui/save`, form)
		return body
	}

	// static with no key: refused, and the reason says why it was needed
	if body := save(`static`, false); !strings.Contains(body, `AKID is required when Credentials-Type is`) {
		t.Errorf("static credentials with no key were accepted:\n%s", body)
	}
	// unset means static, so the same applies
	if body := save(``, false); !strings.Contains(body, `AKID is required when Credentials-Type is`) {
		t.Errorf("an unset Credentials-Type dropped the requirement:\n%s", body)
	}
	// the role based modes do not need one, and must not be blocked
	for _, mode := range []string{`environment`, `ec2role`} {
		if body := save(mode, false); !strings.Contains(body, `Saved`) {
			t.Errorf("%s credentials were blocked for want of a key they do not use:\n%s", mode, body)
		}
	}
	// and static with a key saves
	if body := save(`static`, true); !strings.Contains(body, `Saved`) {
		t.Errorf("a complete static configuration was refused:\n%s", body)
	}
}

// TestFormKeepsAValueTheEnumNoLongerOffers covers what happens when a plugin drops a value
// an existing runner still holds.
//
// A select always has something selected, so an option that is merely not marked falls to
// the first entry.  Left like that, an operator who opened the runner to rename it would
// save a different value for a field they never touched, and for a field others depend on
// through requiredif that also flips their requirements.
func TestFormKeepsAValueTheEnumNoLongerOffers(t *testing.T) {
	h := newHarness(t)
	proto, err := dynamic.MapRunnerDefinition(`SQS`, `SQS`, sqs.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.store.ReplaceKinds(uuid.New(), `edge`, []dynamic.RunnerDefinition{proto}); err != nil {
		t.Fatal(err)
	}

	// a runner holding a mode the plugin no longer declares, stored directly because the
	// form would not let one be created now
	id := uuid.New()
	stored := proto
	stored.Name, stored.UUID = `legacy`, id
	stored.Variables = append([]dynamic.Variable(nil), proto.Variables...)
	for i := range stored.Variables {
		switch stored.Variables[i].Name {
		case `Credentials-Type`:
			stored.Variables[i].Value = `retired-mode`
		case `Queue-URL`:
			stored.Variables[i].Value = `https://sqs.us-east-1.amazonaws.com/1/q`
		case `Region`:
			stored.Variables[i].Value = `us-east-1`
		}
	}
	if err = h.store.PutRunner(stored); err != nil {
		t.Fatal(err)
	}

	_, body := h.get(t, `/ui/edit?uuid=`+id.String())
	// the held value is still offered, still selected, and marked as no longer supported
	if !strings.Contains(body, `<option value="retired-mode" selected>retired-mode (no longer offered)</option>`) {
		t.Errorf("the held value was dropped from the picker, so saving would silently change it:\n%s", body)
	}
	// and none of the declared options is silently selected in its place
	for _, opt := range []string{`static`, `environment`, `ec2role`} {
		if strings.Contains(body, `<option value="`+opt+`" selected>`) {
			t.Errorf("%q was selected in place of the held value", opt)
		}
	}
}

// TestFormAlwaysOffersABlankChoice covers the required select.
//
// A select always has a selection and the browser's required check only fires on an empty
// value, so without a blank entry a required picker starts on its first option and can be
// submitted by someone who never looked at it.
func TestFormAlwaysOffersABlankChoice(t *testing.T) {
	h := newHarness(t)
	proto, err := dynamic.MapRunnerDefinition(`SQS`, `SQS`, sqs.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// make the picker required, the shape no shipped plugin has yet
	for i := range proto.Variables {
		if proto.Variables[i].Name == `Credentials-Type` {
			proto.Variables[i].Required = true
		}
	}
	if err = h.store.ReplaceKinds(uuid.New(), `edge`, []dynamic.RunnerDefinition{proto}); err != nil {
		t.Fatal(err)
	}
	_, body := h.get(t, `/ui/new?kind=SQS`)
	sel := body[strings.Index(body, `<select name="var.Credentials-Type"`):]
	sel = sel[:strings.Index(sel, `</select>`)]
	if !strings.Contains(sel, `<option value="" selected>`) {
		t.Errorf("a required picker has no blank choice, so it starts on a value nobody chose:\n%s", sel)
	}
	if strings.Contains(sel, `<option value="static" selected>`) {
		t.Errorf("the first value was pre-selected:\n%s", sel)
	}
}

// slowIngester connects and answers applyConfig after a delay, which is how an ingester
// on a busy box or a slow disk behaves.
func slowIngester(t *testing.T, h *harness, delay time.Duration) uuid.UUID {
	t.Helper()
	id := uuid.New()
	mux := rpc.NewMux()
	if err := mux.Register(dynamic.MethodApplyConfig, func(context.Context, json.RawMessage) (any, error) {
		time.Sleep(delay)
		return map[string]any{`ok`: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cf := context.WithTimeout(context.Background(), 10*time.Second)
	defer cf()
	s, err := rpc.Dial(ctx, rpc.ClientConfig{
		Webserver: h.ts.URL, Path: client.INGESTERS_CONTROL_URL, Token: testSecret,
		ID: id, Class: `edge`, Handlers: mux, PingInterval: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	// the server has to know what this one can run or the push will not target it
	if err = s.Call(ctx, dynamic.MethodRegisterKinds, dynamic.RegisterKindsRequest{
		ID: id, Class: `edge`, Kinds: []dynamic.RunnerDefinition{proto(t)},
	}, nil); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestPushGivesEachIngesterItsOwnDeadline is the regression guard.
//
// One context built outside the loop meant the timeout was a budget spent across every
// ingester rather than a bound applied to each: with enough of them, or one slow one, the
// ingesters at the back were reported as having refused a configuration they were never
// actually asked about.
func TestPushGivesEachIngesterItsOwnDeadline(t *testing.T) {
	h := newHarness(t)
	// several ingesters, each slow enough that in total they would blow a shared budget
	// many times over.  pushTimeout is 10s, so five of these share it only if the loop is
	// sequential on one context.
	const each = 3 * time.Second
	const count = 5
	for range count {
		slowIngester(t, h, each)
	}
	waitFor(t, `every ingester to be connected`, func() bool {
		_, body := h.get(t, `/ui/status`)
		return strings.Contains(body, `5 ingesters connected`)
	})

	rd := proto(t)
	rd.Name, rd.UUID = `prod`, uuid.New()

	start := time.Now()
	delivered, errs := h.api().Push(rd)
	elapsed := time.Since(start)

	if len(errs) != 0 {
		t.Errorf("healthy ingesters were reported as rejecting the push: %v", errs)
	}
	if delivered != count {
		t.Errorf("delivered to %d of %d ingesters", delivered, count)
	}
	// running them together, so the whole push takes about as long as the slowest one
	// rather than the sum.  Sequentially this would be 15s.
	if elapsed > each*2 {
		t.Errorf("push took %v for %d ingesters at %v each, they were queued behind each other", elapsed, count, each)
	}
}
