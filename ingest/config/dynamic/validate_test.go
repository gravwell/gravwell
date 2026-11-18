/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package dynamic

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"uuid"
)

// verifiedConfig stands in for a plugin config that has an opinion about its own values.
// Interval is the shape that matters: a string that is only meaningful if it parses as a
// duration, which nothing but this Verify can know.
type verifiedConfig struct {
	Ingester_UUID string
	Tag_Name      string
	Interval      string
	Page_Size     int
}

// nestedSpec is a nested member, the shape no shipped plugin has yet and the one the INI
// writer cannot represent.
type nestedSpec struct {
	Field string
	Value string
}

// optionalNestedCfg declares a nested member without requiring one, which is the ordinary
// way such a thing turns up in a plugin config.
type optionalNestedCfg struct {
	Ingester_UUID string
	Tag_Name      string
	Filters       []nestedSpec
}

func (c *verifiedConfig) Verify() error {
	if c.Interval != `` {
		if _, err := time.ParseDuration(c.Interval); err != nil {
			return err
		}
	}
	if c.Tag_Name == `` {
		return errors.New("Tag-Name is required")
	}
	return nil
}

// plainConfig has no Verify, which is allowed: there is simply nothing more to check once
// it has parsed.
type plainConfig struct {
	Ingester_UUID string
	Tag_Name      string
}

// defFor builds a definition the way one would arrive from a webserver.
func defFor(t *testing.T, kind string, v any) RunnerDefinition {
	t.Helper()
	rd, err := MapRunnerDefinition(kind, `instance`, v)
	if err != nil {
		t.Fatal(err)
	}
	if rd.UUID == uuid.Nil() {
		rd.UUID = uuid.New()
	}
	return rd
}

// setVar overwrites a variable the way an operator editing a form would, including with
// something the Go type would never have produced.
func setVar(t *testing.T, rd RunnerDefinition, name string, val any) RunnerDefinition {
	t.Helper()
	out := rd
	out.Variables = append([]Variable(nil), rd.Variables...)
	for i := range out.Variables {
		if out.Variables[i].Name == name {
			out.Variables[i].Value = val
			return out
		}
	}
	t.Fatalf("no variable named %s in %+v", name, rd.Variables)
	return out
}

func TestValidate(t *testing.T) {
	var n NopManager
	if err := n.RegisterKind(`verified`, false, verifiedConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := n.RegisterKind(`plain`, false, plainConfig{}); err != nil {
		t.Fatal(err)
	}

	good := defFor(t, `verified`, verifiedConfig{
		Ingester_UUID: uuid.New().String(),
		Tag_Name:      `test`,
		Interval:      `30s`,
		Page_Size:     100,
	})
	if err := n.Validate(good); err != nil {
		t.Fatalf("a good configuration was rejected: %v", err)
	}

	// the case this exists for: a duration with no unit.  It is a legal string, it makes
	// the round trip through JSON and INI without complaint, and only Verify knows.
	bad := setVar(t, good, `Interval`, `3`)
	err := n.Validate(bad)
	if err == nil {
		t.Fatal(`an interval of "3" was accepted`)
	}
	if !errors.Is(err, ErrInvalidRunner) {
		t.Errorf("error does not wrap ErrInvalidRunner: %v", err)
	}
	if !strings.Contains(err.Error(), `missing unit in duration`) {
		t.Errorf("the plugin's own reason was lost: %v", err)
	}

	// a value of the wrong type never gets as far as Verify, it fails to parse
	if err = n.Validate(setVar(t, good, `Page-Size`, `not a number`)); err == nil {
		t.Error(`a non numeric Page-Size was accepted`)
	} else if !errors.Is(err, ErrInvalidRunner) {
		t.Errorf("error does not wrap ErrInvalidRunner: %v", err)
	}

	// a semantic rule that has nothing to do with parsing is still caught
	if err = n.Validate(setVar(t, good, `Tag-Name`, ``)); err == nil {
		t.Error(`an empty required Tag-Name was accepted`)
	}

	// a variable the plugin does not have is refused rather than quietly dropped, which
	// is the difference between a config that is wrong and one that is silently partial
	extra := good
	extra.Variables = append(append([]Variable(nil), good.Variables...),
		Variable{Name: `Nonsense`, Type: typeString, Value: `x`})
	if err = n.Validate(extra); err == nil {
		t.Error(`a variable the plugin does not have was accepted`)
	}

	// a kind nothing registered cannot be checked, and must not be claimed as fine
	unknown := good
	unknown.Kind = `nothing`
	if err = n.Validate(unknown); err == nil {
		t.Error(`a definition of an unregistered kind was accepted`)
	} else if !errors.Is(err, ErrUnknownKind) {
		t.Errorf("error does not wrap ErrUnknownKind: %v", err)
	}

	// a plugin without a Verify has nothing further to say, which is not a failure
	if err = n.Validate(defFor(t, `plain`, plainConfig{
		Ingester_UUID: uuid.New().String(),
		Tag_Name:      `test`,
	})); err != nil {
		t.Errorf("a config with no Verify was rejected: %v", err)
	}
}

// TestValidateKindNameIsNotAGoIdentifier covers the reason validation renders under a
// fixed section name.  A kind is a free form string and most of them are not legal
// exported Go field names, which is what a section has to become to be parsed back.
func TestValidateKindNameIsNotAGoIdentifier(t *testing.T) {
	var n NopManager
	for _, kind := range []string{`okta`, `ms-graph`, `a kind with spaces`, `123`} {
		if err := n.RegisterKind(kind, false, verifiedConfig{}); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		rd := defFor(t, kind, verifiedConfig{
			Ingester_UUID: uuid.New().String(),
			Tag_Name:      `test`,
			Interval:      `1s`,
		})
		if err := n.Validate(rd); err != nil {
			t.Errorf("kind %q: %v", kind, err)
		}
		if err := n.Validate(setVar(t, rd, `Interval`, `3`)); err == nil {
			t.Errorf("kind %q: a bad interval was accepted", kind)
		}
	}
}

// TestBuildStatuses covers the set that goes up the wire: a rejection wins over the copy
// still running, runners this ingester registered for itself are nobody else's business,
// and the same state twice produces the same report.
func TestBuildStatuses(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	kept := []configuredRunner{
		{Kind: `k`, Name: `remote-ok`, UUID: a, remote: true},
		{Kind: `k`, Name: `remote-bad`, UUID: b, remote: true},
		{Kind: `k`, Name: `local`, UUID: c},
	}
	rejected := map[uuid.UUID]RunnerStatus{
		b: {UUID: b, Kind: `k`, Name: `remote-bad`, Error: `boom`},
	}
	got := buildStatuses(kept, rejected)
	if len(got) != 2 {
		t.Fatalf("reported %d statuses, want 2: %+v", len(got), got)
	}
	byID := map[uuid.UUID]RunnerStatus{}
	for _, rs := range got {
		byID[rs.UUID] = rs
	}
	if st, ok := byID[a]; !ok || !st.OK() {
		t.Errorf("a runner we are carrying should report clean: %+v", st)
	}
	if st, ok := byID[b]; !ok || st.OK() || st.Error != `boom` {
		t.Errorf("a rejection should win over the copy still on disk: %+v", st)
	}
	if _, ok := byID[c]; ok {
		t.Error(`a runner this ingester registered itself should not be reported`)
	}
	// stable ordering, so two reports of the same state are the same report
	if again := buildStatuses(kept, rejected); fmt.Sprint(again) != fmt.Sprint(got) {
		t.Errorf("ordering is not stable:\n%v\n%v", got, again)
	}
}

// TestINIAcceptsAnUnsetNestedMember is the regression guard on refusing too much.
//
// A nested member that cannot be written is only a problem when something was put in it.
// Refusing on the declared type instead made every configuration of a plugin that merely
// declares one unwritable, so the plugin could not be deployed at all: Sync rejects a
// definition it cannot render, so nothing of that kind would ever reach disk.
func TestINIAcceptsAnUnsetNestedMember(t *testing.T) {
	rd, err := MapRunnerDefinition(`Nested`, `beat`, optionalNestedCfg{
		Ingester_UUID: uuid.New().String(),
		Tag_Name:      `test`,
		// Filters left alone
	})
	if err != nil {
		t.Fatal(err)
	}
	// the variable is still described, it just carries no value
	v, ok := findVar(rd, `Filters`)
	if !ok {
		t.Fatal(`the nested member should still be described`)
	}
	if v.Value != nil {
		t.Fatalf("setup is wrong, the member carries a value: %+v", v)
	}

	ini, err := rd.INI()
	if err != nil {
		t.Fatalf("a configuration whose nested member is unset was refused: %v", err)
	}
	if !strings.Contains(ini, "Tag-Name") {
		t.Errorf("the rest of the configuration was not written:\n%s", ini)
	}
	if strings.Contains(ini, `Filters`) {
		t.Errorf("an unset member should not be emitted:\n%s", ini)
	}
}

// TestINIRefusesAPopulatedNestedMember is the other half: once something is in it, the
// block cannot be written and silently leaving it out is what this exists to stop.
func TestINIRefusesAPopulatedNestedMember(t *testing.T) {
	rd, err := MapRunnerDefinition(`Nested`, `beat`, optionalNestedCfg{
		Ingester_UUID: uuid.New().String(),
		Tag_Name:      `test`,
		Filters:       []nestedSpec{{Field: `a`, Value: `b`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ini, err := rd.INI()
	if err == nil {
		t.Fatalf("a populated nested member was silently dropped:\n%s", ini)
	}
	if !errors.Is(err, ErrUnrepresentable) {
		t.Errorf("error does not wrap ErrUnrepresentable: %v", err)
	}
	if !strings.Contains(err.Error(), `Filters`) {
		t.Errorf("the error does not name the member: %v", err)
	}
}

// panickyCfg stands in for a plugin whose Verify blows up on a value an operator typed.
type panickyCfg struct {
	Ingester_UUID string
	Tag_Name      string
}

func (p *panickyCfg) Verify() error {
	if p.Tag_Name == `boom` {
		panic(`plugin Verify indexed past the end of something`)
	}
	return nil
}

// TestValidateSurvivesAPanickingPlugin is the regression guard on the crash.
//
// Validate is the one place this package calls into code it does not own, on data a
// webserver supplied.  Without a recover a plugin that panics takes the ingester down
// from the poll goroutine, and takes it down again on every restart for as long as the
// definition is still on the server.
func TestValidateSurvivesAPanickingPlugin(t *testing.T) {
	var n NopManager
	if err := n.RegisterKind(`panicky`, false, panickyCfg{}); err != nil {
		t.Fatal(err)
	}
	rd, err := MapRunnerDefinition(`panicky`, `beat`, panickyCfg{
		Ingester_UUID: uuid.New().String(), Tag_Name: `boom`,
	})
	if err != nil {
		t.Fatal(err)
	}

	// no recover here on purpose: if Validate lets a panic through, this test fails by
	// crashing the run, which is precisely what it is guarding against
	verr := n.Validate(rd)
	if verr == nil {
		t.Fatal(`a panicking Verify was reported as a valid configuration`)
	}
	if !errors.Is(verr, ErrInvalidRunner) {
		t.Errorf("error does not wrap ErrInvalidRunner: %v", verr)
	}
	if !strings.Contains(verr.Error(), `panic`) {
		t.Errorf("the reason does not say it panicked, so it would be indistinguishable from a normal rejection: %v", verr)
	}

	// and a definition the same plugin is happy with still passes, so the recover has not
	// simply turned the whole kind into a failure
	good, err := MapRunnerDefinition(`panicky`, `beat`, panickyCfg{
		Ingester_UUID: uuid.New().String(), Tag_Name: `fine`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = n.Validate(good); err != nil {
		t.Errorf("a valid configuration was rejected: %v", err)
	}
}

// TestSyncReportsAPanickingPluginRatherThanDying walks the same hazard down the path it
// actually reaches production on: the background poll, where a panic is unrecovered.
func TestSyncReportsAPanickingPluginRatherThanDying(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)
	if err := dcm.RegisterKind(`panicky`, false, panickyCfg{}); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	rd, err := MapRunnerDefinition(`panicky`, `beat`, panickyCfg{
		Ingester_UUID: id.String(), Tag_Name: `boom`,
	})
	if err != nil {
		t.Fatal(err)
	}
	rd.UUID = id
	if err = dcm.Sync([]RunnerDefinition{rd}); err != nil {
		t.Fatalf("Sync failed rather than rejecting the runner: %v", err)
	}
	var found bool
	for _, rs := range dcm.Statuses() {
		if rs.UUID == id {
			found = true
			if rs.OK() {
				t.Errorf("a runner whose Verify panicked reported clean: %+v", rs)
			}
		}
	}
	if !found {
		t.Error(`the runner was not reported at all`)
	}
	// nothing was written, because nothing was accepted
	if ents := confNames(t, dir); len(ents) != 0 {
		t.Errorf("a rejected runner was written to disk: %v", ents)
	}
}
