/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package dynamic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic/rpc"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

var (
	ErrSingletonRegistered = errors.New("singleton runner of the same type already registered")
	ErrUnknownKind         = errors.New("runner kind unsupported, no registration kind found")
	ErrKindRegistered      = errors.New("runner kind already registered")
	ErrRunnerRegistered    = errors.New("runner of the same kind and name already registered")
	ErrInvalidRunner       = errors.New("runner configuration is not usable")
)

// validationSection is the INI section name a definition is rendered under when it is
// being checked rather than written.
//
// A fixed name is used rather than the real kind because the kind has to become a Go
// struct field to be parsed back, and a kind is a free form string: "okta" is a perfectly
// legal kind and not a legal exported field name.  The section header is the one part of
// the rendering that validation does not need to be faithful about, everything that can
// actually be wrong lives in the keys below it.
const validationSection = `Runner`

type Manager interface {
	Start() error
	Close() error
	Signal() <-chan struct{}                             // read only channel
	Load(any) error                                      // load files from disk and apply them to the config object
	RegisterKind(string, bool, any) error                // register a kind that we can run
	RegisterRunner(string, string, uuid.UUID, any) error // register a configured runner
}

type configuredRunner struct {
	RunnerDefinition
	backingFile string // path to backing file
	remote      bool   // came from the webserver rather than from this ingester
}

// registeredKind is a kind registration together with the Go type it was derived from.
//
// The type is the whole point.  A definition that arrives from a webserver has been
// through JSON and describes itself only as strings, ints and bools, which is not enough
// to know whether the plugin can actually run it.  Keeping the type means an incoming
// definition can be rendered, parsed back with the same loader the ingester uses at
// startup, and handed to the plugin's own Verify, which is the only thing that knows an
// Interval of "3" is not a duration.
type registeredKind struct {
	RunnerDefinition
	typ reflect.Type // the plugin's config struct, never a pointer
}

type NopManager struct {
	// mtx guards the two lists.  A DynamicConfigManager syncs from a background
	// goroutine while the ingester is still registering from its main thread, so these
	// are genuinely shared.  The fields stay exported because callers read them, but a
	// caller reading them directly while a sync is running is on its own, use the
	// accessors.
	mtx sync.Mutex

	// Available is the list of definitions that have been registered, only one entry per kind
	Available []registeredKind

	// Configured is a complete list of configured runners, each item must contain a fully
	// Populated Variable block
	Configured []configuredRunner

	running bool
}

func NewNil() Manager {
	return &NopManager{}
}

func (n *NopManager) Start() (err error) {
	n.mtx.Lock()
	if n.running {
		err = errors.New("already running")
	} else {
		n.running = true
	}
	n.mtx.Unlock()
	return
}

// Close always succeeds
func (n *NopManager) Close() error {
	n.mtx.Lock()
	n.running = false
	n.mtx.Unlock()
	return nil
}

// Signal returns a nil channel that will never fire
func (n *NopManager) Signal() (v <-chan struct{}) {
	return
}

// Load will actually load configuration blobs into the object passed in
// NopManager just validates that something sane was passed in
func (n *NopManager) Load(v any) (err error) {
	if v == nil {
		return errors.New("nil object")
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer {
		return errors.New("object must be a pointer")
	} else if rv.IsNil() {
		return errors.New("object is a nil pointer")
	}
	// it has to point at a struct, which is the only thing the config loader can fill in.
	// A pointer to a pointer is the shape this gets handed by mistake, and it has to be
	// refused loudly: it cannot be loaded into, so accepting it would mean a reload that
	// silently applied no configuration at all and reported success.
	if rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("object must be a pointer to a struct, got a pointer to %s", rv.Elem().Kind())
	}
	return nil // all good
}

// RegisterKind takes a type and populates it as a something that this ingester CAN handle
// this function does not take any data in the type, it simply enumerates it into a RunnerDefinition
// without the Name and UUID and populates the Available list
func (n *NopManager) RegisterKind(kind string, singleton bool, v any) (err error) {
	if kind == `` {
		err = errors.New("missing kind")
		return
	} else if v == nil {
		err = errors.New("nil object")
		return
	}

	// MapRunnerDefinition insists on a name, a kind registration does not have one yet,
	// so hand it the kind and strip the name back out
	var rd RunnerDefinition
	if rd, err = MapRunnerDefinition(kind, kind, v); err != nil {
		return
	}
	// a kind describes what we can run, it carries no identity of its own
	rd.Name, rd.UUID = ``, uuid.Nil()
	rd.Singleton = singleton

	n.mtx.Lock()
	defer n.mtx.Unlock()
	// only one entry per kind
	if _, ok := n.lookupKindLocked(rd.Kind); ok {
		err = fmt.Errorf("%w %q", ErrKindRegistered, rd.Kind)
		return
	}
	n.Available = append(n.Available, registeredKind{RunnerDefinition: rd, typ: derefType(reflect.TypeOf(v))})
	return
}

// Validate reports whether a definition is one this ingester could actually run.
//
// It is the same path the configuration takes for real, which is what makes the answer
// worth anything: the definition is rendered to an INI block, parsed back with the loader
// the ingester uses at startup, and handed to the plugin's own Verify.  That catches the
// three separate ways a definition can be wrong, and they are genuinely separate: a value
// of the wrong type never parses, a key the plugin does not have never stores, and a value
// that is fine as a string but meaningless to the plugin is only ever caught by Verify.
//
// A nil return means every one of those passed.
func (n *NopManager) Validate(rd RunnerDefinition) error {
	n.mtx.Lock()
	defer n.mtx.Unlock()
	return n.validateLocked(rd)
}

// validateLocked is Validate with the lock already held.
//
// It recovers, because the last thing it does is call code this package does not own.  A
// plugin's Verify runs on whatever the webserver sent, and a plugin that indexes past the
// end of a string an operator typed would otherwise take the whole ingester down from the
// poll goroutine, every time it restarts, for as long as the definition is on the server.
// The rpc package guards inbound handlers for exactly this reason; this is the same
// hazard reached down the polling path instead.
//
// A definition that makes validation panic is refused, which is the same answer as a
// definition that fails it, and the panic goes into the reason so it is visible rather
// than swallowed.
func (n *NopManager) validateLocked(rd RunnerDefinition) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: validating it panicked: %v", ErrInvalidRunner, r)
		}
	}()
	var rk registeredKind
	var ok bool
	if rk, ok = n.lookupKindLocked(rd.Kind); !ok {
		return fmt.Errorf("%w %q", ErrUnknownKind, rd.Kind)
	} else if rk.typ == nil || rk.typ.Kind() != reflect.Struct {
		// a kind registered from something that is not a struct cannot be checked, and
		// refusing to run it on those grounds would be worse than running it
		return nil
	}

	// render under the substitute section name, see validationSection
	probe := rd
	probe.Kind = validationSection
	if probe.Name == `` {
		probe.Name = validationSection // INI insists on a name, this one is thrown away
	}
	var blob string
	if blob, err = probe.INI(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRunner, err)
	}

	// struct{ Runner map[string]*T }, which is the shape gcfg maps a named section onto
	holder := reflect.New(reflect.StructOf([]reflect.StructField{{
		Name: validationSection,
		Type: reflect.MapOf(reflect.TypeFor[string](), reflect.PointerTo(rk.typ)),
	}}))
	if err = config.LoadConfigBytes(holder.Interface(), []byte(blob)); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRunner, err)
	}

	// exactly one subsection went in, so exactly one should have come back out
	set := holder.Elem().Field(0)
	if set.Len() != 1 {
		return fmt.Errorf("%w: produced %d configurations, want 1", ErrInvalidRunner, set.Len())
	}
	for _, k := range set.MapKeys() {
		cur := set.MapIndex(k)
		if cur.IsNil() {
			return fmt.Errorf("%w: produced no configuration", ErrInvalidRunner)
		}
		// Verify is optional, a plugin that does not have one has nothing further to say
		if v, ok := cur.Interface().(interface{ Verify() error }); ok {
			if err = v.Verify(); err != nil {
				return fmt.Errorf("%w: %w", ErrInvalidRunner, err)
			}
		}
	}
	return nil
}

// Kinds returns a copy of the registered kinds, safe to read while a sync is running.
func (n *NopManager) Kinds() (r []RunnerDefinition) {
	n.mtx.Lock()
	defer n.mtx.Unlock()
	r = make([]RunnerDefinition, 0, len(n.Available))
	for _, rk := range n.Available {
		r = append(r, rk.RunnerDefinition)
	}
	return
}

// KindNames returns just the names of the registered kinds.
func (n *NopManager) KindNames() (r []string) {
	n.mtx.Lock()
	defer n.mtx.Unlock()
	r = make([]string, 0, len(n.Available))
	for _, rd := range n.Available {
		r = append(r, rd.Kind)
	}
	return
}

// lookupKind finds the registration for a kind.
func (n *NopManager) lookupKind(kind string) (rk registeredKind, ok bool) {
	n.mtx.Lock()
	defer n.mtx.Unlock()
	return n.lookupKindLocked(kind)
}

// lookupKindLocked is lookupKind with the lock already held.  The Available list carries
// at most one entry per kind so the first hit is the only hit.
func (n *NopManager) lookupKindLocked(kind string) (rk registeredKind, ok bool) {
	for _, cur := range n.Available {
		if cur.Kind == kind {
			rk, ok = cur, true
			return
		}
	}
	return
}

// RegisterRunner registers a complete runner with a given name, kind, UUID, and fully populated config block
// If the UUID is empty one is generated, the kind is validated agains the Available list, if no
// available registration is present RegisterRunner rejects the registration.
// If the registered kind is marked as a singleton and an existing Kind already exists
// the registration is rejected.  Upon successful registration the the complete RunnerDefinition is
// translated to a INI block and written to a .conf file in the storage directory
func (n *NopManager) RegisterRunner(name, kind string, guid uuid.UUID, v any) (err error) {
	n.mtx.Lock()
	defer n.mtx.Unlock()
	var rd RunnerDefinition
	if rd, _, err = n.prepareRunnerLocked(name, kind, guid, v); err == nil {
		// the NopManager has nowhere to put the INI block, it just remembers the runner
		n.Configured = append(n.Configured, configuredRunner{RunnerDefinition: rd})
	}
	return
}

// prepareRunner validates a registration against the current state and resolves it into a
// complete definition plus the INI block it renders to.  It deliberately records nothing,
// a manager with storage has to get the config onto disk before it can claim the runner is
// registered, so the caller owns the append to Configured.
func (n *NopManager) prepareRunnerLocked(name, kind string, guid uuid.UUID, v any) (rd RunnerDefinition, ini string, err error) {
	if name == `` {
		err = errors.New("missing name")
		return
	} else if kind == `` {
		err = errors.New("missing kind")
		return
	} else if v == nil {
		err = errors.New("nil object")
		return
	}
	// the kind has to be one we advertised, we cannot run something we do not know about
	var def registeredKind
	var ok bool
	if def, ok = n.lookupKindLocked(kind); !ok {
		err = fmt.Errorf("%w %q", ErrUnknownKind, kind)
		return
	}

	if rd, err = MapRunnerDefinition(kind, name, v); err != nil {
		return
	}

	// a singleton kind can only be configured once, everything else just has to be
	// uniquely named within its kind
	for _, cur := range n.Configured {
		if cur.Kind != rd.Kind {
			continue
		} else if def.Singleton {
			err = fmt.Errorf("%w %q", ErrSingletonRegistered, rd.Kind)
			return
		} else if cur.Name == rd.Name {
			err = fmt.Errorf("%w %q %q", ErrRunnerRegistered, rd.Kind, rd.Name)
			return
		}
	}

	// an explicitly provided UUID wins, then one lifted out of the config, then a new one
	if guid != uuid.Nil() {
		rd.UUID = guid
	} else if rd.UUID == uuid.Nil() {
		rd.UUID = uuid.New()
	}
	rd.Singleton = def.Singleton

	// render it now, a config that cannot be represented in an INI block is rejected
	// here rather than at write time so that every manager fails the same way
	ini, err = rd.INI()
	return
}

type DynamicConfigManager struct {
	// NopManager supplies the kind registry, the validation path and the mutex that
	// guards both lists.  Its Available and Configured members stay exported because the
	// tests read them, but nothing outside this package should: a reader that does not
	// hold the embedded mutex races the sync goroutine, use Kinds and Statuses instead.
	NopManager
	Config // embed the config
	guid   uuid.UUID
	lgr    *log.Logger
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// ch carries the reload signal to whoever is driving the ingester.  It is buffered
	// by one, a burst of changes should be one reload.
	ch chan struct{}

	// nudge wakes the background client when something changed locally, so a kind
	// registered after we connected is declared without waiting for the poll.
	nudge chan struct{}

	// handlers are the methods a webserver may call on us, principally a config push.
	handlers *rpc.Mux

	// pollInterval is how often we ask for our configuration, a field rather than the
	// constant so that tests do not have to wait on it.
	pollInterval time.Duration

	// statuses is the verdict on every configuration the webserver has handed us, as of
	// the last sync.  It is guarded by the embedded NopManager's mutex along with the
	// lists it is derived from, because it has to be rebuilt in the same critical section
	// that decides what is on disk or the two can disagree.
	statuses []RunnerStatus

	// loadErrors are the configurations that would not load off disk at the last Load.
	//
	// They are held apart from the sync because they are found at a different time and
	// answer a different question.  A sync says whether a definition is one we would
	// accept; a load says whether the file we wrote actually comes back.  A file can pass
	// the first and fail the second, and when it does that is the fact worth reporting,
	// so these are merged over the top of the sync's verdict rather than under it.
	//
	// The set is replaced wholesale by each Load, which is what clears an entry: a
	// configuration that has been fixed simply stops appearing.
	loadErrors map[uuid.UUID]RunnerStatus
}

func NewDynamicConfigManager(ctx context.Context, c Config, guid uuid.UUID, lgr *log.Logger) (m Manager, err error) {
	if err = c.Verify(); err != nil {
		return
	}
	// Verify only checks the contents of a block that is turned on: a config with nothing
	// in it is legitimately "no dynamic configuration" and passes.  That is fine as an
	// answer to "is this block valid" and useless as an answer to "can this manager run",
	// which is the question being asked here.  Without this the background loop reaches
	// for a webserver out of an empty list and takes the process down with it.
	if len(c.Webserver) == 0 {
		err = errors.New("dynamic configuration has no webserver endpoints, it cannot run")
		return
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if lgr == nil {
		lgr = log.NewDiscardLogger()
	}

	// validate the local config

	dcm := &DynamicConfigManager{
		Config:       c,
		guid:         guid,
		lgr:          lgr,
		ch:           make(chan struct{}, 1),
		nudge:        make(chan struct{}, 1),
		pollInterval: c.PollInterval(),
	}
	// our own cancellable view of the caller's context, so Close stops the background
	// client without the caller having to cancel anything
	dcm.ctx, dcm.cancel = context.WithCancel(ctx)

	// a webserver may push a config rather than waiting for us to poll
	dcm.handlers = rpc.NewMux()
	if err = dcm.handlers.Register(MethodApplyConfig, func(c context.Context, params json.RawMessage) (any, error) {
		return dcm.applyConfig(c, params)
	}); err != nil {
		dcm.cancel()
		return nil, err
	}

	m = dcm
	return
}

// Signal returns the channel that fires when the configuration on disk has changed.  The
// caller should reload its configuration and hand the result back to Load.
func (dcm *DynamicConfigManager) Signal() <-chan struct{} {
	return dcm.ch
}

// Close stops the background client and waits for it to finish.
func (dcm *DynamicConfigManager) Close() error {
	dcm.cancel()
	dcm.wg.Wait()
	return nil
}

func (dcm *DynamicConfigManager) Start() (err error) {
	dcm.mtx.Lock()
	if dcm.running {
		err = errors.New("already running")
	} else {
		dcm.start()
		dcm.running = true
	}
	dcm.mtx.Unlock()
	return
}

// RegisterKind registers a kind and wakes the background client so that a kind registered
// after we connected is declared without waiting for the next poll.
func (dcm *DynamicConfigManager) RegisterKind(kind string, singleton bool, v any) (err error) {
	if err = dcm.NopManager.RegisterKind(kind, singleton, v); err != nil {
		return
	}
	select {
	case dcm.nudge <- struct{}{}:
	default:
	}
	return
}

// Load applies every configuration in the storage directory on top of v.
//
// A file that cannot be loaded is skipped rather than fatal.  That is the whole point of
// doing this here instead of calling config.LoadConfigOverlays: the overlay loader stops
// at the first file it cannot parse and returns an error, which an ingester turns into a
// refusal to start.  One bad configuration then takes the whole ingester down, including
// every other configuration that was fine, and it stays down until somebody with shell
// access finds the file and deletes it.  A configuration arrives from a webserver, so
// that is a way for one bad edit to strand a fleet.
//
// Each file is tried on its own first and only applied once it is known to load, so a
// file that fails never leaves a half applied section behind in v.  The reason is
// recorded against the runner the file belongs to and goes upstream on the next status
// report, which is what turns "the ingester will not start" into "this runner is broken,
// and here is why".
func (dcm *DynamicConfigManager) Load(v any) (err error) {
	// use the embedded NopManager to do the any object validation because we are lazy
	if err = dcm.NopManager.Load(v); err != nil {
		return
	}

	var files []string
	if files, err = overlayFiles(dcm.Storage); err != nil {
		// the directory itself is unusable, which is not something skipping a file fixes
		return
	}

	failures := map[uuid.UUID]RunnerStatus{}
	var loaded int
	for _, pth := range files {
		if lerr := loadOne(v, pth); lerr != nil {
			dcm.lgr.Error("dynamic config skipping a configuration that will not load",
				log.KV("file", pth), log.KVErr(lerr))
			dcm.recordLoadFailure(failures, pth, lerr)
			continue
		}
		loaded++
	}

	dcm.setLoadFailures(failures)
	if len(failures) > 0 {
		dcm.lgr.Warn("dynamic config loaded with failures",
			log.KV("loaded", loaded), log.KV("failed", len(failures)))
		// get the reasons upstream rather than sitting on them until the next poll
		dcm.nudgeClient()
	}
	return nil
}

// maxConfigBytes matches the ceiling ingest/config puts on a single configuration file.
// It is restated here because loadOne reads the bytes itself rather than handing the path
// to a loader that would check it.
const maxConfigBytes = 4 * 1024 * 1024

// loadOne applies one config file to v, or leaves v untouched and says why not.
//
// The file is read once and the same bytes are parsed twice: first into a throwaway of
// v's own type to find out whether they load at all, and only then into v.  Both halves
// of that matter.  gcfg populates as it parses, so a file that fails halfway has already
// written whatever preceded the failure, which is why the trial cannot run against the
// live configuration.  And the trial has to be of the same bytes rather than of the same
// path, or a file rewritten between the two reads is checked in one state and applied in
// another, which puts the half applied section back exactly where the trial was meant to
// stop it: a sync running on the background goroutine writes into this directory, so
// that race is ordinary rather than theoretical.
//
// Having parsed once, the second parse cannot fail for anything in the bytes, so v is
// only ever touched by content already known to be good.
func loadOne(v any, pth string) error {
	rt := reflect.TypeOf(v)
	if rt == nil || rt.Kind() != reflect.Pointer {
		return errors.New("object must be a pointer")
	}
	fi, err := os.Stat(pth)
	if err != nil {
		return err
	} else if fi.Size() > maxConfigBytes {
		return fmt.Errorf("configuration is %d bytes, over the %d byte limit", fi.Size(), maxConfigBytes)
	}
	blob, err := os.ReadFile(pth)
	if err != nil {
		return err
	}
	probe := reflect.New(rt.Elem())
	if err = config.LoadConfigBytes(probe.Interface(), blob); err != nil {
		return err
	}
	// it parses.  That is not the same as it being usable: a tag name full of punctuation
	// or a duration with no unit is a perfectly good string and only the plugin that has
	// to run it knows otherwise.  The probe holds exactly what this one file introduced,
	// so asking it now is what keeps the answer to one configuration.
	if err = verifyLoaded(probe.Interface()); err != nil {
		return err
	}
	if err = config.LoadConfigBytes(v, blob); err != nil {
		return err
	}
	// Verify is not a pure check, it is also where a plugin fills in its defaults: a
	// request rate, a lookback, a trimmed host.  Those landed on the probe, and the parse
	// above put the raw, un-defaulted values into v.  Most plugin constructors never call
	// Verify themselves, so a runner built from v would divide by a zero request rate.
	// Lay the verified entries over the raw ones so v carries what the plugin will run.
	mergeVerified(derefValue(reflect.ValueOf(v)), probe.Elem(), 0)
	return nil
}

// mergeVerified copies the plugin configurations held in src over the matching entries of
// dst.  It walks the same shape verifyStructMembers does, so every map entry that was
// verified is the one that ends up in the live configuration.
func mergeVerified(dst, src reflect.Value, depth int) {
	if depth > maxStructDepth || !dst.IsValid() || !src.IsValid() ||
		dst.Kind() != reflect.Struct || src.Kind() != reflect.Struct || dst.Type() != src.Type() {
		return
	}
	rt := dst.Type()
	for i := range rt.NumField() {
		f, dv, sv := rt.Field(i), dst.Field(i), src.Field(i)
		if f.Anonymous && derefType(f.Type).Kind() == reflect.Struct {
			mergeVerified(derefValue(dv), derefValue(sv), depth+1)
			continue
		}
		if !f.IsExported() {
			continue
		}
		switch dv.Kind() {
		case reflect.Map:
			if sv.Len() == 0 || !dv.CanSet() {
				continue
			}
			if dv.IsNil() {
				dv.Set(reflect.MakeMap(dv.Type()))
			}
			for _, k := range sv.MapKeys() {
				dv.SetMapIndex(k, sv.MapIndex(k))
			}
		case reflect.Struct:
			mergeVerified(dv, sv, depth+1)
		case reflect.Pointer:
			if !dv.IsNil() && !sv.IsNil() {
				mergeVerified(dv.Elem(), sv.Elem(), depth+1)
			}
		}
	}
}

// verifyLoaded runs the plugins' own Verify over everything a configuration introduced.
//
// It walks rather than being told what to look for.  The target type is the thing that
// already knows which plugins exist and what shape they are held in, so a plugin added
// later is checked with no change here, and nothing has to resolve a section name back to
// a Go type.  Only map entries are verified, because that is how a named plugin
// configuration is held, and the value being walked came from one file into a zero value,
// so the only entries present are the ones that file created.
//
// It recovers for the same reason validateLocked does: Verify is somebody else's code
// running on whatever was deployed, and a panic in it must cost one configuration rather
// than the whole ingester.
func verifyLoaded(v any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("verifying it panicked: %v", r)
		}
	}()
	rv := derefValue(reflect.ValueOf(v))
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return nil
	}
	return verifyStructMembers(rv, 0)
}

// verifyStructMembers walks a config struct looking for plugin configurations to verify.
func verifyStructMembers(rv reflect.Value, depth int) (err error) {
	if depth > maxStructDepth || !rv.IsValid() || rv.Kind() != reflect.Struct {
		return nil
	}
	rt := rv.Type()
	for i := range rt.NumField() {
		f, fv := rt.Field(i), rv.Field(i)
		// an embedded struct promotes its members, and the plugin set is embedded, so
		// recurse before the export check the way the rest of this package does
		if f.Anonymous && derefType(f.Type).Kind() == reflect.Struct {
			if err = verifyStructMembers(derefValue(fv), depth+1); err != nil {
				return
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		switch fv.Kind() {
		case reflect.Map:
			if err = verifyMapMembers(f.Name, fv); err != nil {
				return
			}
		case reflect.Struct, reflect.Pointer:
			if err = verifyStructMembers(derefValue(fv), depth+1); err != nil {
				return
			}
		}
	}
	return
}

// verifyMapMembers verifies each configuration held in a named map, which is how every
// plugin that can be configured more than once is carried.
func verifyMapMembers(field string, fv reflect.Value) error {
	for _, k := range fv.MapKeys() {
		ev := fv.MapIndex(k)
		switch ev.Kind() {
		case reflect.Pointer, reflect.Interface:
			if ev.IsNil() {
				continue
			}
		}
		if !ev.CanInterface() {
			continue
		}
		if vf, ok := ev.Interface().(interface{ Verify() error }); ok {
			if verr := vf.Verify(); verr != nil {
				return fmt.Errorf("%s %q: %w", field, k.String(), verr)
			}
		}
	}
	return nil
}

// overlayFiles lists the config files in a storage directory, in a stable order.
//
// A missing directory is not an error, it just means nothing has been deployed yet, which
// is the same thing LoadConfigOverlays does.
func overlayFiles(pth string) (r []string, err error) {
	if pth == `` {
		return
	}
	var fi os.FileInfo
	if fi, err = os.Stat(pth); err != nil {
		if os.IsNotExist(err) {
			err = nil
		}
		return
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("storage %q is not a directory", pth)
	}
	var dents []os.DirEntry
	if dents, err = os.ReadDir(pth); err != nil {
		return nil, fmt.Errorf("failed to read storage %q %w", pth, err)
	}
	for _, dent := range dents {
		if !dent.Type().IsRegular() || filepath.Ext(dent.Name()) != confExt {
			continue
		}
		r = append(r, filepath.Join(pth, dent.Name()))
	}
	sort.Strings(r) // ReadDir is already sorted, this says that it has to stay that way
	return
}

// recordLoadFailure files the reason a config could not be loaded against the runner it
// belongs to.
//
// The runner is identified from the file name, which is where the UUID is: the in memory
// list of configured runners is empty at startup, which is exactly when this matters
// most.  A file that does not carry a UUID cannot be reported against anything, so it is
// left to the log the caller already wrote.
func (dcm *DynamicConfigManager) recordLoadFailure(into map[uuid.UUID]RunnerStatus, pth string, err error) {
	id, rest, ok := parseRunnerFile(pth)
	if !ok {
		return
	}
	kind, name := dcm.splitKindName(rest)
	into[id] = RunnerStatus{
		UUID:  id,
		Kind:  kind,
		Name:  name,
		Error: fmt.Sprintf("failed to load %s: %v", filepath.Base(pth), err),
	}
}

// splitKindName separates the kind from the name in the middle of a config file name.
//
// It cannot be done by looking for a separator.  fnameChunk keeps underscores, so both
// halves may contain one and "My_Kind_my_runner" has four readings, only one of them
// right.  The registered kinds are what settle it: the longest one that prefixes this is
// the kind, and the rest is the name.
//
// When nothing matches there is no honest split to make, so none is made and the whole
// thing is reported as the name.  That is the case on the very first load of a process,
// before any kind has been registered, and a label that is merely unsplit beats one that
// confidently names the wrong kind.
func (dcm *DynamicConfigManager) splitKindName(rest string) (kind, name string) {
	var best string
	for _, k := range dcm.KindNames() {
		chunk := fnameChunk(k)
		if chunk == `` || len(chunk) >= len(rest) {
			continue
		}
		if strings.HasPrefix(rest, chunk+`_`) && len(chunk) > len(best) {
			best = chunk
			kind, name = k, rest[len(chunk)+1:]
		}
	}
	if best == `` {
		return ``, rest
	}
	return
}

// parseRunnerFile pulls the UUID back out of a file name written by runnerPath, which
// builds them as kind_name_uuid.conf, and hands back the kind_name part unsplit.
//
// The UUID is taken from the end rather than by counting fields from the front, because
// both the kind and the name may contain an underscore: fnameChunk keeps them.  That same
// ambiguity is why the rest is returned as it stands rather than being split here, see
// splitKindName, which has the registered kinds to settle it with.
func parseRunnerFile(pth string) (id uuid.UUID, rest string, ok bool) {
	base := strings.TrimSuffix(filepath.Base(pth), confExt)
	idx := strings.LastIndex(base, `_`)
	if idx < 0 {
		return
	}
	var err error
	if id, err = uuid.Parse(base[idx+1:]); err != nil {
		return
	}
	return id, base[:idx], true
}

// setLoadFailures replaces what the last load made of the directory and refreshes the
// reported set so the failures are visible without waiting for a sync.
func (dcm *DynamicConfigManager) setLoadFailures(failures map[uuid.UUID]RunnerStatus) {
	dcm.mtx.Lock()
	defer dcm.mtx.Unlock()
	dcm.loadErrors = failures
}

// nudgeClient wakes the background client without blocking if it is already awake.
func (dcm *DynamicConfigManager) nudgeClient() {
	select {
	case dcm.nudge <- struct{}{}:
	default:
	}
}

func (dcm *DynamicConfigManager) RegisterRunner(name, kind string, guid uuid.UUID, v any) (err error) {
	dcm.mtx.Lock()
	defer dcm.mtx.Unlock()
	var rd RunnerDefinition
	var iniContent string
	if rd, iniContent, err = dcm.prepareRunnerLocked(name, kind, guid, v); err != nil {
		return
	}
	// generate filename as kind_name_guid.conf, use the resolved UUID rather than the one
	// handed in, prepareRunner may have generated it or lifted it out of the config
	fname := fmt.Sprintf("%s_%s_%v.conf", fnameChunk(kind), fnameChunk(name), rd.UUID)
	pth := filepath.Join(dcm.Storage, fname)
	if err = writeConfFile(pth, iniContent); err != nil {
		return
	}
	// only claim the runner once its config is actually on disk.  These are runners the
	// ingester itself reported, so they are not remote and a server that does not know
	// about them must not cause them to be deleted.
	dcm.Configured = append(dcm.Configured, configuredRunner{RunnerDefinition: rd, backingFile: pth})
	return
}

// writeConfFile writes an INI block to pth by way of a temporary file so that a partially
// written config is never visible to a loader.  The temporary file must not end in .conf,
// overlay loading consumes anything in the storage directory that does.
func writeConfFile(pth, ini string) (err error) {
	tmp := pth + `.temp`
	var fout *os.File
	if fout, err = os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0660); err != nil {
		return
	}
	if _, err = io.WriteString(fout, ini); err != nil {
		fout.Close()
	} else if err = fout.Close(); err == nil {
		if err = os.Rename(tmp, pth); err == nil {
			return // the only path that leaves a file behind
		}
	}
	os.Remove(tmp) // best effort, we are already returning an error
	return
}

// fnameChunk is just a helper that removes spaces, non-printable characters, and any characters that
// cannot be part of a file name or may traditionally be part of a file path (like /, \, etc...)
func fnameChunk(v string) (r string) {
	// an allow list rather than a block list, the set of characters that are safe in a
	// file name on every platform we ship to is far smaller and far more stable than the
	// set that is not.  A dot is excluded too, it is what makes .. and an extension
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return -1 // drop it
		}
		return r
	}, v)
}
