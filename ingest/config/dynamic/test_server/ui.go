/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
	"uuid"

	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic/icon"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic/server"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

//go:embed assets
var assets embed.FS

// refreshLists tells the page which fragments to re-fetch after a change, the small
// stand in for htmx in assets/hx.js acts on it the way real htmx acts on HX-Trigger.
const refreshLists = `#kinds,#runners`

// UI serves the HTML interface.  Every response is a fragment of HTML rendered on the
// server, the page holds no state of its own.
type UI struct {
	api   *server.API
	store *Store
	lgr   *log.Logger
	tpl   *template.Template
}

func NewUI(api *server.API, store *Store, lgr *log.Logger) (u *UI, err error) {
	if lgr == nil {
		lgr = log.NewDiscardLogger()
	}
	var tpl *template.Template
	if tpl, err = template.ParseFS(assets, `assets/index.html`); err != nil {
		return nil, fmt.Errorf("failed to parse templates %w", err)
	}
	return &UI{api: api, store: store, lgr: lgr, tpl: tpl}, nil
}

// Register mounts every UI route on a mux.
func (u *UI) Register(mux *http.ServeMux) {
	// the stylesheet and the htmx stand in are embedded, so this serves with no network
	// access and no build step
	if static, err := fs.Sub(assets, `assets`); err == nil {
		mux.Handle(`GET /static/`, http.StripPrefix(`/static/`, http.FileServer(http.FS(static))))
	} else {
		u.lgr.Error("failed to mount static assets", log.KVErr(err))
	}
	mux.HandleFunc(`GET /{$}`, u.index)
	mux.HandleFunc(`GET /ui/kinds`, u.kinds)
	mux.HandleFunc(`GET /ui/runners`, u.runners)
	mux.HandleFunc(`GET /ui/runnerstatus`, u.runnerStatus)
	mux.HandleFunc(`GET /ui/status`, u.status)
	mux.HandleFunc(`GET /ui/ingesters`, u.ingesters)
	mux.HandleFunc(`GET /ui/listrow`, u.listRow)
	mux.HandleFunc(`GET /ui/new`, u.newRunner)
	mux.HandleFunc(`GET /ui/edit`, u.editRunner)
	mux.HandleFunc(`GET /ui/unmanaged`, u.unmanagedRunner)
	mux.HandleFunc(`POST /ui/save`, u.save)
	mux.HandleFunc(`POST /ui/delete`, u.del)
}

// runnerView is a configured runner as the list draws it: what it is, and what the
// ingesters carrying it currently make of it.
type runnerView struct {
	UUID    string
	Kind    string
	Name    string
	State   string // ok, bad, or unknown
	Detail  string // the one line an operator reads without opening anything
	Icon    template.HTML
	HasIcon bool

	// Unmanaged marks a row that exists only because an ingester reported on it.  There
	// is no stored definition behind it, so it opens the reports rather than the form,
	// see unmanagedRunners.
	Unmanaged bool
}

// kindView is a registered kind as the menu draws it.
type kindView struct {
	Kind      string
	Vars      int
	Singleton bool
	Version   string // empty when the plugin does not declare one
	Icon      template.HTML
	HasIcon   bool
	Docs      []dynamic.DocLink
}

// iconFor renders a kind's icon, if it has one that survives sanitizing.  The markup is
// rebuilt from an allow list rather than trusted, see icon.Sanitize: an icon is drawn by
// whoever wrote the ingester and arrives here over the wire.
//
// This is the only place the sanitizer's output becomes template.HTML.  icon.Sanitize hands
// back a plain string on purpose, so that a caller doing something other than templating
// does not inherit a type that means "already trusted"; earning that type is the one thing
// this function is for.
func iconFor(md *dynamic.RunnerMetadata) (template.HTML, bool) {
	if md == nil {
		return ``, false
	}
	svg, ok := icon.Sanitize(md.Icon)
	if !ok {
		return ``, false
	}
	return template.HTML(svg), true
}

// versionOf renders a plugin's version, or nothing when it does not declare one.  A zero
// version is not "0.0.0", it is an absence, and printing it as a number would be a claim
// the plugin never made.
func versionOf(md *dynamic.RunnerMetadata) string {
	if md == nil || !md.Version.Enabled() {
		return ``
	}
	return md.Version.String()
}

// statusView is one ingester's standing with one runner, as the detail panel draws it.
//
// It covers both halves of the question.  Reported is what that ingester last said, and
// an ingester the runner is tasked to but which has never said anything gets a row too:
// silence from a machine that is meant to be running something is a fact worth seeing,
// and it is the only thing there is to see while a fleet is offline.
type statusView struct {
	Ingester  string
	Class     string
	State     string // ok, bad, or unknown when nothing has been reported
	Error     string
	Since     string
	Updated   string
	Reported  bool
	Connected bool
}

// taskedTo is every ingester the server knows of that this runner is meant for.
//
// It is answered from what ingesters have registered, which is on disk, rather than from
// who happens to be connected.  That is the whole point: a runner is tasked to an ingester
// the moment the assignment matches, and an operator needs to see that whether or not the
// ingester is up.  Without it a configuration created while a fleet is offline looks like
// it is going nowhere.
//
// The rule is dynamic.RunnerQuery.Matches, the same one the ingester's own poll uses, so
// what the interface says a runner is tasked to is what the ingester would actually be
// handed.
func taskedTo(rd dynamic.RunnerDefinition, known []server.Ingester) (r []server.Ingester) {
	for _, ing := range known {
		q := dynamic.RunnerQuery{ID: ing.UUID, Class: ing.Class, Kinds: ing.Kinds}
		if q.Matches(rd) {
			r = append(r, ing)
		}
	}
	return
}

// statusStates are the three things the interface can say about a runner, and the order
// matters: an error outranks everything, and never having been reported on is not the
// same as being fine.
const (
	stateOK      = `ok`
	stateBad     = `bad`
	stateUnknown = `unknown`
)

// rollUp reduces every ingester's report about one runner to the single state and line
// the list shows.
//
// One ingester failing is the whole runner failing.  A configuration that four ingesters
// accept and a fifth rejects is a broken configuration, and averaging that away is how a
// green screen ends up lying to somebody.
func rollUp(rows []server.StatusRow, tasked int, connected int) (state, detail string) {
	var bad int
	var first string
	for _, r := range rows {
		if r.OK() {
			continue
		}
		bad++
		if first == `` {
			first = r.Error
		}
	}
	// a stale report is still the truth about what that ingester last made of this, but
	// it is not news, and reading "accepted" next to an ingester that has been down for a
	// week is how a screen ends up lying
	stale := ``
	if connected == 0 && (tasked > 0 || len(rows) > 0) {
		stale = ` · none connected`
	}

	switch {
	case bad == 1 && len(rows) == 1:
		return stateBad, first + stale
	case bad > 0:
		return stateBad, fmt.Sprintf("%d of %s rejected it: %s%s", bad, plural(len(rows), `report`), first, stale)
	case tasked == 0 && len(rows) == 0:
		// not a silence to wait out: nothing that has registered could run this, so
		// either the assignment names something that is not there or no ingester
		// advertising this kind has ever connected
		return stateUnknown, `no registered ingester matches this assignment`
	case len(rows) == 0:
		return stateUnknown, fmt.Sprintf("tasked to %s, none has reported yet%s",
			plural(tasked, `ingester`), stale)
	case len(rows) < tasked:
		return stateOK, fmt.Sprintf("accepted by %d of %s%s", len(rows), plural(tasked, `tasked ingester`), stale)
	}
	return stateOK, fmt.Sprintf("accepted by %s%s", plural(len(rows), `ingester`), stale)
}

// plural renders a count with its noun, so the interface does not say "1 ingesters".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// ago renders a timestamp the way an operator reads one, which is as a distance from now
// rather than as a wall clock time they then have to subtract in their head.
func ago(t time.Time) string {
	if t.IsZero() {
		return `never`
	}
	d := time.Since(t)
	switch {
	case d < 0:
		return `just now` // a clock that stepped backwards, do not print a negative age
	case d < 2*time.Second:
		return `just now`
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// formView is what the form template renders from.
type formView struct {
	Kind     string
	Name     string
	UUID     string
	Existing bool
	Fields   []field
	Note     *note

	// the kind's own description, so the form says what is being configured rather than
	// only naming it
	Icon    template.HTML
	HasIcon bool
	Version string
	Docs    []dynamic.DocLink

	// Targets are the ingesters that registered this kind, the only ones worth pinning a
	// configuration of it to.  Selected marks the ones this runner is already pinned to.
	Targets []targetView
	// Classes are the classes that have been seen, plus any this runner already names
	// even if no ingester is currently carrying them.
	Classes []classView
	// OtherClasses holds classes typed in by hand that are not in the known list.
	OtherClasses string
}

type targetView struct {
	UUID     string
	Class    string
	Kinds    string
	Selected bool
	Known    bool // false when the runner names a UUID that never registered this kind
}

type classView struct {
	Name     string
	Selected bool
}

type note struct {
	Class string // ok or bad
	Text  string
}

func (u *UI) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set(`Content-Type`, `text/html; charset=utf-8`)
	if err := u.tpl.ExecuteTemplate(w, name, data); err != nil {
		u.lgr.Error("failed to render", log.KV("template", name), log.KVErr(err))
	}
}

// fail renders an error into wherever the caller was targeting.  A failed action shows up
// in the interface rather than as a status code nobody sees.
func (u *UI) fail(w http.ResponseWriter, err error) {
	w.Header().Set(`Content-Type`, `text/html; charset=utf-8`)
	u.render(w, `note`, note{Class: `bad`, Text: err.Error()})
}

func (u *UI) index(w http.ResponseWriter, r *http.Request) {
	u.render(w, `page`, nil)
}

func (u *UI) kinds(w http.ResponseWriter, r *http.Request) {
	kinds, err := u.store.Kinds()
	if err != nil {
		u.fail(w, err)
		return
	}
	out := make([]kindView, 0, len(kinds))
	for _, rd := range kinds {
		kv := kindView{
			Kind:      rd.Kind,
			Vars:      len(rd.Variables),
			Singleton: rd.Singleton,
			Version:   versionOf(rd.Metadata),
		}
		kv.Icon, kv.HasIcon = iconFor(rd.Metadata)
		if rd.Metadata != nil {
			kv.Docs = rd.Metadata.Documentation
		}
		out = append(out, kv)
	}
	u.render(w, `kinds`, out)
}

func (u *UI) runners(w http.ResponseWriter, r *http.Request) {
	runners, err := u.store.Runners()
	if err != nil {
		u.fail(w, err)
		return
	}
	// one query for every status rather than one per runner, the list is polled
	statuses, err := u.store.Statuses()
	if err != nil {
		u.fail(w, err)
		return
	}
	byRunner := map[uuid.UUID][]server.StatusRow{}
	for _, st := range statuses {
		byRunner[st.Runner] = append(byRunner[st.Runner], st)
	}
	// who the server knows about, from the registrations on disk rather than from who is
	// connected, so a runner still says where it is meant to go while a fleet is down
	known, err := u.store.Ingesters()
	if err != nil {
		u.fail(w, err)
		return
	}
	live := u.connectedSet()
	// a configured runner carries no metadata of its own, it was built from a form.  The
	// icon belongs to its kind, so it is looked up rather than stored twice.
	meta, err := u.store.KindMetadata()
	if err != nil {
		u.fail(w, err)
		return
	}
	// sanitize each kind's icon once rather than once per runner using it
	icons := map[string]template.HTML{}
	for kind, md := range meta {
		if ic, ok := iconFor(md); ok {
			icons[kind] = ic
		}
	}
	out := make([]runnerView, 0, len(runners))
	for _, rd := range runners {
		tasked := taskedTo(rd, known)
		state, detail := rollUp(byRunner[rd.UUID], len(tasked), countLive(tasked, live))
		rv := runnerView{
			UUID:   rd.UUID.String(),
			Kind:   rd.Kind,
			Name:   rd.Name,
			State:  state,
			Detail: detail,
		}
		rv.Icon, rv.HasIcon = icons[rd.Kind], icons[rd.Kind] != ``
		out = append(out, rv)
		delete(byRunner, rd.UUID) // accounted for, so it is not reported again below
	}
	// whatever is left was reported by an ingester about something this server holds no
	// definition for.  It is listed rather than dropped, see unmanagedRunners.
	out = append(out, unmanagedRunners(byRunner, icons)...)
	u.render(w, `runners`, out)
}

// unmanagedRunners is the rows an ingester reported that no stored definition accounts for.
//
// They used to be collected and then silently discarded, because the list was built by
// walking the runners table and looking each one's reports up.  A report about anything
// else fell through the gap, and the case that falls through it is the one an operator most
// needs: a configuration file sitting in an ingester's storage directory that this server
// did not put there, or did not put there in this shape.  A hand edited file is the obvious
// way to get one, and it is exactly the file that will not load.
//
// The ingester is right to send these and the server is right to keep them.  "An ingester
// is failing to load something, and I have no idea what it is" is a fact, and a screen that
// draws only what it already knows about cannot tell anybody.
//
// Kind and Name come off the report itself.  They are the ingester's reading of a file
// name, so they are labels rather than identity, which is the other reason these rows open
// the reports instead of the form: there is nothing here that could be edited and saved.
func unmanagedRunners(byRunner map[uuid.UUID][]server.StatusRow,
	icons map[string]template.HTML) (out []runnerView) {
	for id, rows := range byRunner {
		if len(rows) == 0 {
			continue
		}
		// nothing is tasked to it as far as this server is concerned, it is not something
		// this server assigned, so the roll up is told only what was reported
		state, detail := rollUp(rows, 0, len(rows))
		rv := runnerView{
			UUID:      id.String(),
			Kind:      firstNonEmpty(rows, func(r server.StatusRow) string { return r.Kind }),
			Name:      firstNonEmpty(rows, func(r server.StatusRow) string { return r.Name }),
			State:     state,
			Detail:    detail,
			Unmanaged: true,
		}
		if rv.Name == `` {
			rv.Name = id.String() // something has to be clickable
		}
		rv.Icon, rv.HasIcon = icons[rv.Kind], icons[rv.Kind] != ``
		out = append(out, rv)
	}
	// the map has no order, and this list is polled every few seconds
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].UUID < out[j].UUID
	})
	return
}

// firstNonEmpty is the first label any ingester managed to put on a report.  Two ingesters
// reporting different names for one UUID is not worth arbitrating over: either is a better
// thing to show an operator than a bare UUID.
func firstNonEmpty(rows []server.StatusRow, pick func(server.StatusRow) string) string {
	for _, r := range rows {
		if v := pick(r); v != `` {
			return v
		}
	}
	return ``
}

// unmanagedView is the detail panel for a runner this server has heard about but does not
// hold, see unmanagedRunners.
type unmanagedView struct {
	UUID  string
	Kind  string
	Name  string
	Known bool // whether the kind is one some ingester has registered
}

// unmanagedRunner draws the reports for a UUID that has no stored definition.
//
// It is a separate handler from editRunner rather than a mode of it because there is
// nothing to edit: no definition means no fields, and a form whose Save would create a
// second runner under the same UUID is a trap rather than a convenience.  What an operator
// needs here is which ingester is complaining and what it said, which is what this shows.
func (u *UI) unmanagedRunner(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.URL.Query().Get(`uuid`))
	if err != nil {
		u.fail(w, fmt.Errorf("invalid runner id %w", err))
		return
	}
	// if a definition has appeared since the list was drawn, this is an ordinary runner
	// and the form is the right thing to show
	if _, err = u.store.Runner(id); err == nil {
		u.editRunner(w, r)
		return
	} else if !errors.Is(err, server.ErrNotFound) {
		u.fail(w, err)
		return
	}
	rows, err := u.store.RunnerStatuses(id)
	if err != nil {
		u.fail(w, err)
		return
	}
	if len(rows) == 0 {
		// it cleared itself between the list being drawn and this being opened, which is
		// the outcome the panel tells the operator to wait for
		u.render(w, `gone`, nil)
		return
	}
	uv := unmanagedView{
		UUID: id.String(),
		Kind: firstNonEmpty(rows, func(r server.StatusRow) string { return r.Kind }),
		Name: firstNonEmpty(rows, func(r server.StatusRow) string { return r.Name }),
	}
	if uv.Kind != `` {
		if _, kerr := u.store.Kind(uv.Kind); kerr == nil {
			uv.Known = true
		}
	}
	u.render(w, `unmanaged`, uv)
}

// connectedSet is the ingesters with a session right now, for telling a live report from
// the last thing a machine said before it went away.
func (u *UI) connectedSet() map[uuid.UUID]bool {
	live := map[uuid.UUID]bool{}
	for _, s := range u.api.Connected() {
		live[s.ID()] = true
	}
	return live
}

// countLive is how many of a set are currently connected.
func countLive(set []server.Ingester, live map[uuid.UUID]bool) (n int) {
	for _, ing := range set {
		if live[ing.UUID] {
			n++
		}
	}
	return
}

// runnerStatus draws the detail panel for one runner: every ingester that has reported on
// it, what it said, and when.  It is its own fragment so that it can refresh on a timer
// while an operator sits on the form watching a fix take effect.
func (u *UI) runnerStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.URL.Query().Get(`uuid`))
	if err != nil {
		u.fail(w, fmt.Errorf("invalid runner id %w", err))
		return
	}
	rows, err := u.store.RunnerStatuses(id)
	if err != nil {
		u.fail(w, err)
		return
	}
	known, err := u.store.Ingesters()
	if err != nil {
		u.fail(w, err)
		return
	}
	// the class an ingester authenticated with is worth showing next to its UUID, it is
	// usually the thing an operator actually recognizes
	classes := map[uuid.UUID]string{}
	for _, ing := range known {
		classes[ing.UUID] = ing.Class
	}
	live := u.connectedSet()

	out := make([]statusView, 0, len(rows))
	reported := map[uuid.UUID]bool{}
	for _, row := range rows {
		reported[row.Ingester] = true
		sv := statusView{
			Ingester:  row.Ingester.String(),
			Class:     classes[row.Ingester],
			State:     stateOK,
			Error:     row.Error,
			Since:     ago(row.Since),
			Updated:   ago(row.Updated),
			Reported:  true,
			Connected: live[row.Ingester],
		}
		if !row.OK() {
			sv.State = stateBad
		}
		out = append(out, sv)
	}

	// and the ones this runner is tasked to that have said nothing.  Reading the runner
	// back rather than trusting the status rows is what makes this work offline: the
	// assignment and the registrations are both on disk, so the set can be worked out with
	// nothing connected at all.
	if rd, rerr := u.store.Runner(id); rerr == nil {
		for _, ing := range taskedTo(rd, known) {
			if reported[ing.UUID] {
				continue
			}
			out = append(out, statusView{
				Ingester:  ing.UUID.String(),
				Class:     ing.Class,
				State:     stateUnknown,
				Since:     ago(ing.LastSeen),
				Updated:   `never`,
				Connected: live[ing.UUID],
			})
		}
	}
	u.render(w, `runnerstatus`, out)
}

func (u *UI) status(w http.ResponseWriter, r *http.Request) {
	u.render(w, `status`, struct{ Connected int }{len(u.api.Connected())})
}

// ingesters renders the dropdown behind the connection chip: who is connected right now,
// with the UUID and class each one authenticated with.
func (u *UI) ingesters(w http.ResponseWriter, r *http.Request) {
	live := u.connectedSet()
	known, err := u.store.Ingesters()
	if err != nil {
		u.fail(w, err)
		return
	}
	// how much each one has been given to run.  Answered from the stored runners and the
	// stored registrations, so it is just as true of an ingester that is switched off:
	// "this box is meant to be running four things" is the question an operator opens
	// this list to ask, and it does not stop mattering when the box goes away.
	runners, err := u.store.Runners()
	if err != nil {
		u.fail(w, err)
		return
	}
	tasked := map[uuid.UUID]int{}
	for _, rd := range runners {
		for _, ing := range taskedTo(rd, known) {
			tasked[ing.UUID]++
		}
	}

	type row struct {
		UUID      string
		Class     string
		Kinds     string
		Tasked    int
		Connected bool
		LastSeen  string
	}
	out := make([]row, 0, len(known))
	for _, ing := range known {
		out = append(out, row{
			UUID:      ing.UUID.String(),
			Class:     ing.Class,
			Kinds:     strings.Join(ing.Kinds, `, `),
			Tasked:    tasked[ing.UUID],
			Connected: live[ing.UUID],
			LastSeen:  ing.LastSeen.Format(time.RFC3339),
		})
	}
	u.render(w, `ingesters`, out)
}

// listRow renders one empty input for a list control, which is what the + button appends.
//
// The name is echoed back into the field, and html/template escapes it, so there is
// nothing to smuggle through here.  A name that does not match a variable the prototype
// declares is ignored at save time anyway, the prototype is the schema.
func (u *UI) listRow(w http.ResponseWriter, r *http.Request) {
	u.render(w, `listrow`, r.URL.Query().Get(`name`))
}

// assignmentView builds the two pickers for a kind, marking whatever cur is already
// pinned to.
func (u *UI) assignmentView(kind string, cur *dynamic.RunnerDefinition) (targets []targetView, classes []classView, other string, err error) {
	var known []server.Ingester
	if known, err = u.store.KindIngesters(kind); err != nil {
		return
	}
	selectedIDs := map[uuid.UUID]bool{}
	selectedClasses := map[string]bool{}
	if cur != nil && cur.Assigned != nil {
		for _, id := range cur.Assigned.UUIDs {
			selectedIDs[id] = true
		}
		for _, c := range cur.Assigned.Classes {
			selectedClasses[c] = true
		}
	}

	seen := map[uuid.UUID]bool{}
	for _, ing := range known {
		seen[ing.UUID] = true
		targets = append(targets, targetView{
			UUID:     ing.UUID.String(),
			Class:    ing.Class,
			Kinds:    strings.Join(ing.Kinds, `, `),
			Selected: selectedIDs[ing.UUID],
			Known:    true,
		})
	}
	// a runner may name an ingester that has not registered this kind, usually because it
	// has not connected since the server was started.  Show it rather than silently
	// dropping it on the next save.
	for id := range selectedIDs {
		if !seen[id] {
			targets = append(targets, targetView{UUID: id.String(), Selected: true})
		}
	}

	var knownClasses []string
	if knownClasses, err = u.store.Classes(); err != nil {
		return
	}
	inList := map[string]bool{}
	for _, c := range knownClasses {
		inList[c] = true
		classes = append(classes, classView{Name: c, Selected: selectedClasses[c]})
	}
	// classes this runner names that nothing is currently carrying stay editable as text
	var extras []string
	for c := range selectedClasses {
		if !inList[c] {
			extras = append(extras, c)
		}
	}
	sort.Strings(extras)
	other = strings.Join(extras, `, `)
	return
}

// newRunner draws an empty form for a registered kind.
func (u *UI) newRunner(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get(`kind`)
	proto, err := u.store.Kind(kind)
	if err != nil {
		u.fail(w, err)
		return
	}
	targets, classes, other, err := u.assignmentView(proto.Kind, nil)
	if err != nil {
		u.fail(w, err)
		return
	}
	fv := formView{
		Kind:         proto.Kind,
		UUID:         uuid.New().String(), // a new runner gets its identity up front
		Fields:       fieldsFor(proto, nil),
		Targets:      targets,
		Classes:      classes,
		OtherClasses: other,
	}
	u.describe(&fv, proto)
	u.render(w, `form`, fv)
}

// editRunner draws a form filled in from a configured runner.
func (u *UI) editRunner(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.URL.Query().Get(`uuid`))
	if err != nil {
		u.fail(w, fmt.Errorf("invalid runner id %w", err))
		return
	}
	cur, err := u.store.Runner(id)
	if err != nil {
		u.fail(w, err)
		return
	}
	proto, err := u.store.Kind(cur.Kind)
	if err != nil {
		// the runner outlived its registration, which happens when an ingester that
		// could run it is not connected.  Fall back to describing it from itself so the
		// operator can still see and edit what is stored.
		proto = cur
	}
	targets, classes, other, err := u.assignmentView(cur.Kind, &cur)
	if err != nil {
		u.fail(w, err)
		return
	}
	fv := formView{
		Kind:         cur.Kind,
		Name:         cur.Name,
		UUID:         cur.UUID.String(),
		Existing:     true,
		Fields:       fieldsFor(proto, &cur),
		Targets:      targets,
		Classes:      classes,
		OtherClasses: other,
	}
	u.describe(&fv, proto)
	u.render(w, `form`, fv)
}

// save creates or updates a runner and pushes it to whatever is connected.
func (u *UI) save(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		u.fail(w, err)
		return
	}
	kind := r.Form.Get(`kind`)
	name := strings.TrimSpace(r.Form.Get(`name`))
	if name == `` {
		u.fail(w, fmt.Errorf("a runner needs a name"))
		return
	}
	proto, err := u.store.Kind(kind)
	if err != nil {
		u.fail(w, err)
		return
	}
	id, err := uuid.Parse(r.Form.Get(`uuid`))
	if err != nil {
		u.fail(w, fmt.Errorf("invalid runner id %w", err))
		return
	}
	// the runner as it stands, if any.  A secret that the form left blank keeps whatever
	// is already stored, because the form never showed it in the first place.
	var cur *dynamic.RunnerDefinition
	if existing, lerr := u.store.Runner(id); lerr == nil {
		cur = &existing
	}

	// the prototype is the schema, never the form, so a submitted field that the
	// ingester never advertised cannot become part of the config
	vars, err := parseForm(proto, r.Form, cur)
	if err != nil {
		u.fail(w, err)
		return
	}
	assigned, err := parseAssignment(r.Form)
	if err != nil {
		u.fail(w, err)
		return
	}
	rd := dynamic.RunnerDefinition{
		Kind:      proto.Kind,
		Name:      name,
		UUID:      id,
		Singleton: proto.Singleton,
		Variables: vars,
		Assigned:  assigned,
	}
	// render it the way the ingester will, so a config that cannot be written as an INI
	// is refused here rather than failing at the far end
	if _, err = rd.INI(); err != nil {
		u.fail(w, fmt.Errorf("this configuration cannot be represented: %w", err))
		return
	}
	if err = u.store.PutRunner(rd); err != nil {
		u.fail(w, err)
		return
	}

	delivered, pushErrs := u.api.Push(rd)
	n := &note{Class: `ok`}
	switch {
	case len(pushErrs) > 0:
		n.Class = `bad`
		n.Text = fmt.Sprintf("Saved, but %d ingester(s) rejected it: %s",
			len(pushErrs), strings.Join(pushErrs, `; `))
	case delivered > 0:
		n.Text = fmt.Sprintf("Saved and pushed to %d connected ingester(s).", delivered)
	default:
		n.Text = `Saved. No ingester is connected, so nothing was pushed.`
	}

	targets, classes, other, err := u.assignmentView(rd.Kind, &rd)
	if err != nil {
		u.fail(w, err)
		return
	}
	w.Header().Set(`X-Refresh`, refreshLists)
	fv := formView{
		Kind:         rd.Kind,
		Name:         rd.Name,
		UUID:         rd.UUID.String(),
		Existing:     true,
		Fields:       fieldsFor(proto, &rd),
		Targets:      targets,
		Classes:      classes,
		OtherClasses: other,
		Note:         n,
	}
	u.describe(&fv, proto)
	u.render(w, `form`, fv)
}

// describe fills in the part of a form that comes from the kind rather than from the
// runner: its icon, its version and where to read about it.  The prototype is the only
// thing carrying that, a configured runner was built from a form and has none of it.
func (u *UI) describe(fv *formView, proto dynamic.RunnerDefinition) {
	fv.Icon, fv.HasIcon = iconFor(proto.Metadata)
	fv.Version = versionOf(proto.Metadata)
	if proto.Metadata != nil {
		fv.Docs = proto.Metadata.Documentation
	}
}

// parseAssignment builds the assignment from the two pickers plus the free text box.
//
// An empty selection means unassigned, which is how a configuration goes to everything
// that can run it.  Nothing here trusts the browser for anything but the values
// themselves: a UUID that does not parse is refused rather than quietly dropped, because
// silently widening who receives a configuration is the wrong way to fail.
func parseAssignment(form url.Values) (a *dynamic.Assignment, err error) {
	out := &dynamic.Assignment{}
	for _, raw := range form[`assign.uuid`] {
		raw = strings.TrimSpace(raw)
		if raw == `` {
			continue
		}
		var id uuid.UUID
		if id, err = uuid.Parse(raw); err != nil {
			return nil, fmt.Errorf("invalid assignment UUID %q %w", raw, err)
		}
		if !containsUUID(out.UUIDs, id) {
			out.UUIDs = append(out.UUIDs, id)
		}
	}
	for _, c := range form[`assign.class`] {
		if c = strings.TrimSpace(c); c != `` && !containsString(out.Classes, c) {
			out.Classes = append(out.Classes, c)
		}
	}
	// classes typed by hand, for one that no ingester is carrying yet
	for _, c := range strings.FieldsFunc(form.Get(`assign.otherclasses`), func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	}) {
		if c = strings.TrimSpace(c); c != `` && !containsString(out.Classes, c) {
			out.Classes = append(out.Classes, c)
		}
	}
	sort.Strings(out.Classes)
	if out.Empty() {
		return nil, nil // unassigned, do not carry an empty struct around
	}
	return out, nil
}

func containsUUID(set []uuid.UUID, v uuid.UUID) bool {
	return slices.Contains(set, v)
}

func containsString(set []string, v string) bool {
	return slices.Contains(set, v)
}

// del removes a configured runner.
func (u *UI) del(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.URL.Query().Get(`uuid`))
	if err != nil {
		u.fail(w, fmt.Errorf("invalid runner id %w", err))
		return
	}
	if err = u.store.DeleteRunner(id); err != nil {
		u.fail(w, err)
		return
	}
	w.Header().Set(`X-Refresh`, refreshLists)
	u.render(w, `note`, note{Class: `ok`, Text: `Runner deleted.`})
}
