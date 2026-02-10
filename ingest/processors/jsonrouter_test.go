/*************************************************************************
* Copyright 2026 Gravwell, Inc. All rights reserved.
* Contact: <legal@gravwell.io>
*
* This software may be modified and distributed under the terms of the
* BSD 2-clause license. See the LICENSE file for details.
**************************************************************************/

package processors

import (
	"fmt"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var (
	jrc = JsonRouteConfig{
		Route_Key: `action`,
		Route:     []string{`login:logintag`, `logout:logouttag`, `error:`},
	}
)

func TestJsonRouteConfig(t *testing.T) {
	if _, rts, err := jrc.validate(); err != nil {
		t.Fatal(err)
	} else if len(rts) != 3 {
		t.Fatal("bad route count")
	}
}

func TestNewJsonRouter(t *testing.T) {
	var tg testTagger
	//build out the default tag
	if _, err := tg.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}
	rc := jrc
	//make a new one
	jr, err := NewJsonRouter(jrc, &tg)
	if err != nil {
		t.Fatal(err)
	}
	//reconfigure the existing
	rc.Route = append(rc.Route, `testA:A`)
	if err = jr.Config(rc, &tg); err != nil {
		t.Fatal(err)
	}
	if tg.mp[`logintag`] != 1 || tg.mp[`logouttag`] != 2 || tg.mp[`A`] != 3 {
		t.Fatalf("bad tag negotiations: %+v", tg.mp)
	}
}

func TestJsonRouterProcess(t *testing.T) {
	var tagger testTagger
	//build out the default tag
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}
	rc := jrc
	rc.Route = append(rc.Route, `dropme:`)
	jr, err := NewJsonRouter(rc, &tagger)
	if err != nil {
		t.Fatal(err)
	}
	testSet := []testTagSet{
		testTagSet{data: `login`, tag: `logintag`, drop: false},
		testTagSet{data: `logout`, tag: `logouttag`, drop: false},
		testTagSet{data: `error`, tag: ``, drop: true},
		testTagSet{data: `dropme`, tag: ``, drop: true},
		testTagSet{data: `unknown`, tag: `default`, drop: false},
	}

	for _, v := range testSet {
		ent := makeJsonTestEntry(v.data)
		if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatal(err)
		} else if v.drop && len(set) != 0 {
			t.Fatalf("invalid drop status on %+v: %d", v, len(set))
		} else if !v.drop && len(set) != 1 {
			t.Fatalf("invalid drop status: %d", len(set))
		} else if tg, ok := tagger.mp[v.tag]; !ok && !v.drop {
			t.Fatalf("tagger didn't create tag %v", v.tag)
		} else if tg != ent.Tag && !v.drop {
			t.Fatalf("Invalid tag results: %v != %v", tg, ent.Tag)
		}
	}

	//make an entry that is not valid JSON
	ent := makeJsonTestEntry(``)
	ent.Data = []byte("not valid json")
	if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
		t.Fatal(err)
	} else if len(set) != 1 {
		t.Fatal("Failed to hit default on count")
	} else if set[0].Tag != tagger.mp[`default`] {
		t.Fatal("Failed to hit default tag")
	}

	//try again with dropping on misses
	rc.Drop_Misses = true
	if jr, err = NewJsonRouter(rc, &tagger); err != nil {
		t.Fatal(err)
	}
	//check with the item that will completely miss
	if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
		t.Fatal(err)
	} else if len(set) != 0 {
		t.Fatal("Failed to drop on invalid JSON")
	}
	//try with one that will have valid JSON but not match routes or drops
	ent = makeJsonTestEntry(`testval`)
	if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
		t.Fatal(err)
	} else if len(set) != 0 {
		t.Fatal("Failed to drop on unmatched value")
	}
}

func TestJsonRouterProcessNestedKey(t *testing.T) {
	var tagger testTagger
	//build out the default tag
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}

	// Test with a nested key path
	rc := JsonRouteConfig{
		Route_Key: `user.role`,
		Route:     []string{`admin:admintag`, `user:usertag`},
	}

	jr, err := NewJsonRouter(rc, &tagger)
	if err != nil {
		t.Fatal(err)
	}

	testData := []struct {
		body string
		tag  string
		drop bool
	}{
		{body: `{"user":{"role":"admin"}}`, tag: `admintag`, drop: false},
		{body: `{"user":{"role":"user"}}`, tag: `usertag`, drop: false},
		{body: `{"user":{"role":"guest"}}`, tag: `default`, drop: false},
		{body: `{"user":{}}`, tag: `default`, drop: false},
		{body: `{}`, tag: `default`, drop: false},
	}

	for i, td := range testData {
		ent := &entry.Entry{
			Tag:  0,
			SRC:  testIP,
			TS:   testTime,
			Data: []byte(td.body),
		}
		if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatalf("Test %d: %v", i, err)
		} else if td.drop && len(set) != 0 {
			t.Fatalf("Test %d: invalid drop status", i)
		} else if !td.drop && len(set) != 1 {
			t.Fatalf("Test %d: invalid drop status: %d", i, len(set))
		} else if tg, ok := tagger.mp[td.tag]; !ok && !td.drop {
			t.Fatalf("Test %d: tagger didn't create tag %v", i, td.tag)
		} else if tg != ent.Tag && !td.drop {
			t.Fatalf("Test %d: Invalid tag results: %v != %v", i, tg, ent.Tag)
		}
	}
}

func makeJsonTestEntry(action string) *entry.Entry {
	return &entry.Entry{
		Tag:  0,
		SRC:  testIP,
		TS:   testTime,
		Data: []byte(fmt.Sprintf(`{"action":"%s","user":"testuser","timestamp":1234567890}`, action)),
	}
}

func TestJsonRouterSpecialCharactersInFieldNames(t *testing.T) {
	var tagger testTagger
	//build out the default tag
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}

	// Test with field names containing dots
	rc := JsonRouteConfig{
		Route_Key: `"field.with.dots"`,
		Route:     []string{`value1:tag1`, `value2:tag2`},
	}

	jr, err := NewJsonRouter(rc, &tagger)
	if err != nil {
		t.Fatal(err)
	}

	// JSON with a field name that contains dots
	testData := []struct {
		body string
		tag  string
	}{
		{body: `{"field.with.dots":"value1"}`, tag: `tag1`},
		{body: `{"field.with.dots":"value2"}`, tag: `tag2`},
		{body: `{"field.with.dots":"other"}`, tag: `default`},
	}

	for i, td := range testData {
		ent := &entry.Entry{
			Tag:  0,
			SRC:  testIP,
			TS:   testTime,
			Data: []byte(td.body),
		}
		if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatalf("Test %d: %v", i, err)
		} else if len(set) != 1 {
			t.Fatalf("Test %d: expected 1 entry, got %d", i, len(set))
		} else if tg, ok := tagger.mp[td.tag]; !ok {
			t.Fatalf("Test %d: tagger didn't create tag %v", i, td.tag)
		} else if tg != ent.Tag {
			t.Fatalf("Test %d: Invalid tag results: %v != %v", i, tg, ent.Tag)
		}
	}
}

func TestJsonRouterFieldNamesWithSpaces(t *testing.T) {
	var tagger testTagger
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}

	// Test with field names containing spaces
	rc := JsonRouteConfig{
		Route_Key: `"field with spaces"`,
		Route:     []string{`admin:admintag`, `user:usertag`},
	}

	jr, err := NewJsonRouter(rc, &tagger)
	if err != nil {
		t.Fatal(err)
	}

	testData := []struct {
		body string
		tag  string
	}{
		{body: `{"field with spaces":"admin"}`, tag: `admintag`},
		{body: `{"field with spaces":"user"}`, tag: `usertag`},
		{body: `{"field with spaces":"guest"}`, tag: `default`},
	}

	for i, td := range testData {
		ent := &entry.Entry{
			Tag:  0,
			SRC:  testIP,
			TS:   testTime,
			Data: []byte(td.body),
		}
		if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatalf("Test %d: %v", i, err)
		} else if len(set) != 1 {
			t.Fatalf("Test %d: expected 1 entry, got %d", i, len(set))
		} else if tg, ok := tagger.mp[td.tag]; !ok {
			t.Fatalf("Test %d: tagger didn't create tag %v", i, td.tag)
		} else if tg != ent.Tag {
			t.Fatalf("Test %d: Invalid tag results: %v != %v", i, tg, ent.Tag)
		}
	}
}

func TestJsonRouterFieldNamesWithSpecialChars(t *testing.T) {
	var tagger testTagger
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}

	// Test with field names containing special characters
	rc := JsonRouteConfig{
		Route_Key: `"field@special!chars#"`,
		Route:     []string{`critical:criticaltag`, `warning:warningtag`},
	}

	jr, err := NewJsonRouter(rc, &tagger)
	if err != nil {
		t.Fatal(err)
	}

	testData := []struct {
		body string
		tag  string
	}{
		{body: `{"field@special!chars#":"critical"}`, tag: `criticaltag`},
		{body: `{"field@special!chars#":"warning"}`, tag: `warningtag`},
		{body: `{"field@special!chars#":"info"}`, tag: `default`},
	}

	for i, td := range testData {
		ent := &entry.Entry{
			Tag:  0,
			SRC:  testIP,
			TS:   testTime,
			Data: []byte(td.body),
		}
		if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatalf("Test %d: %v", i, err)
		} else if len(set) != 1 {
			t.Fatalf("Test %d: expected 1 entry, got %d", i, len(set))
		} else if tg, ok := tagger.mp[td.tag]; !ok {
			t.Fatalf("Test %d: tagger didn't create tag %v", i, td.tag)
		} else if tg != ent.Tag {
			t.Fatalf("Test %d: Invalid tag results: %v != %v", i, tg, ent.Tag)
		}
	}
}

func TestJsonRouterNestedFieldWithSpecialChars(t *testing.T) {
	var tagger testTagger
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}

	// Test with nested paths where one field has special characters
	rc := JsonRouteConfig{
		Route_Key: `user."role.level"`,
		Route:     []string{`superadmin:supertag`, `admin:admintag`},
	}

	jr, err := NewJsonRouter(rc, &tagger)
	if err != nil {
		t.Fatal(err)
	}

	testData := []struct {
		body string
		tag  string
	}{
		{body: `{"user":{"role.level":"superadmin"}}`, tag: `supertag`},
		{body: `{"user":{"role.level":"admin"}}`, tag: `admintag`},
		{body: `{"user":{"role.level":"user"}}`, tag: `default`},
	}

	for i, td := range testData {
		ent := &entry.Entry{
			Tag:  0,
			SRC:  testIP,
			TS:   testTime,
			Data: []byte(td.body),
		}
		if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatalf("Test %d: %v", i, err)
		} else if len(set) != 1 {
			t.Fatalf("Test %d: expected 1 entry, got %d", i, len(set))
		} else if tg, ok := tagger.mp[td.tag]; !ok {
			t.Fatalf("Test %d: tagger didn't create tag %v", i, td.tag)
		} else if tg != ent.Tag {
			t.Fatalf("Test %d: Invalid tag results: %v != %v", i, tg, ent.Tag)
		}
	}
}

func TestJsonRouterFieldNamesWithQuotes(t *testing.T) {
	var tagger testTagger
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}

	// Test with field names that would contain quotes in JSON (escaped)
	// In the config, we need to use Go string escaping
	rc := JsonRouteConfig{
		Route_Key: `"field\"with\"quotes"`,
		Route:     []string{`yes:yestag`, `no:notag`},
	}

	jr, err := NewJsonRouter(rc, &tagger)
	if err != nil {
		t.Fatal(err)
	}

	testData := []struct {
		body string
		tag  string
	}{
		// In actual JSON, the field name needs to be escaped
		{body: `{"field\"with\"quotes":"yes"}`, tag: `yestag`},
		{body: `{"field\"with\"quotes":"no"}`, tag: `notag`},
		{body: `{"field\"with\"quotes":"maybe"}`, tag: `default`},
	}

	for i, td := range testData {
		ent := &entry.Entry{
			Tag:  0,
			SRC:  testIP,
			TS:   testTime,
			Data: []byte(td.body),
		}
		if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatalf("Test %d: %v", i, err)
		} else if len(set) != 1 {
			t.Fatalf("Test %d: expected 1 entry, got %d", i, len(set))
		} else if tg, ok := tagger.mp[td.tag]; !ok {
			t.Fatalf("Test %d: tagger didn't create tag %v", i, td.tag)
		} else if tg != ent.Tag {
			t.Fatalf("Test %d: Invalid tag results: %v != %v", i, tg, ent.Tag)
		}
	}
}

func TestJsonRouterFieldNamesWithBackslashes(t *testing.T) {
	var tagger testTagger
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}

	// Test with field names containing backslashes
	rc := JsonRouteConfig{
		Route_Key: `"path\\to\\field"`,
		Route:     []string{`windows:wintag`, `linux:linuxtag`},
	}

	jr, err := NewJsonRouter(rc, &tagger)
	if err != nil {
		t.Fatal(err)
	}

	testData := []struct {
		body string
		tag  string
	}{
		{body: `{"path\\to\\field":"windows"}`, tag: `wintag`},
		{body: `{"path\\to\\field":"linux"}`, tag: `linuxtag`},
		{body: `{"path\\to\\field":"mac"}`, tag: `default`},
	}

	for i, td := range testData {
		ent := &entry.Entry{
			Tag:  0,
			SRC:  testIP,
			TS:   testTime,
			Data: []byte(td.body),
		}
		if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatalf("Test %d: %v", i, err)
		} else if len(set) != 1 {
			t.Fatalf("Test %d: expected 1 entry, got %d", i, len(set))
		} else if tg, ok := tagger.mp[td.tag]; !ok {
			t.Fatalf("Test %d: tagger didn't create tag %v", i, td.tag)
		} else if tg != ent.Tag {
			t.Fatalf("Test %d: Invalid tag results: %v != %v", i, tg, ent.Tag)
		}
	}
}

func TestJsonRouterComplexNestedPath(t *testing.T) {
	var tagger testTagger
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}

	// Test with complex nested path mixing quoted and unquoted segments
	rc := JsonRouteConfig{
		Route_Key: `data."event.type".severity`,
		Route:     []string{`high:hightag`, `medium:mediumtag`, `low:lowtag`},
	}

	jr, err := NewJsonRouter(rc, &tagger)
	if err != nil {
		t.Fatal(err)
	}

	testData := []struct {
		body string
		tag  string
	}{
		{body: `{"data":{"event.type":{"severity":"high"}}}`, tag: `hightag`},
		{body: `{"data":{"event.type":{"severity":"medium"}}}`, tag: `mediumtag`},
		{body: `{"data":{"event.type":{"severity":"low"}}}`, tag: `lowtag`},
		{body: `{"data":{"event.type":{"severity":"unknown"}}}`, tag: `default`},
	}

	for i, td := range testData {
		ent := &entry.Entry{
			Tag:  0,
			SRC:  testIP,
			TS:   testTime,
			Data: []byte(td.body),
		}
		if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatalf("Test %d: %v", i, err)
		} else if len(set) != 1 {
			t.Fatalf("Test %d: expected 1 entry, got %d", i, len(set))
		} else if tg, ok := tagger.mp[td.tag]; !ok {
			t.Fatalf("Test %d: tagger didn't create tag %v", i, td.tag)
		} else if tg != ent.Tag {
			t.Fatalf("Test %d: Invalid tag results: %v != %v", i, tg, ent.Tag)
		}
	}
}

func TestJsonRouterUnicodeFieldNames(t *testing.T) {
	var tagger testTagger
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}

	// Test with Unicode field names
	rc := JsonRouteConfig{
		Route_Key: `用户.角色`,
		Route:     []string{`管理员:admintag`, `用户:usertag`},
	}

	jr, err := NewJsonRouter(rc, &tagger)
	if err != nil {
		t.Fatal(err)
	}

	testData := []struct {
		body string
		tag  string
	}{
		{body: `{"用户":{"角色":"管理员"}}`, tag: `admintag`},
		{body: `{"用户":{"角色":"用户"}}`, tag: `usertag`},
		{body: `{"用户":{"角色":"访客"}}`, tag: `default`},
	}

	for i, td := range testData {
		ent := &entry.Entry{
			Tag:  0,
			SRC:  testIP,
			TS:   testTime,
			Data: []byte(td.body),
		}
		if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatalf("Test %d: %v", i, err)
		} else if len(set) != 1 {
			t.Fatalf("Test %d: expected 1 entry, got %d", i, len(set))
		} else if tg, ok := tagger.mp[td.tag]; !ok {
			t.Fatalf("Test %d: tagger didn't create tag %v", i, td.tag)
		} else if tg != ent.Tag {
			t.Fatalf("Test %d: Invalid tag results: %v != %v", i, tg, ent.Tag)
		}
	}
}

func TestJsonRouterDuplicateRouteKeys(t *testing.T) {
	var tagger testTagger
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}

	// Test case 1: Duplicate keys in routing map
	rc := JsonRouteConfig{
		Route_Key: `action`,
		Route:     []string{`login:logintag`, `login:anothertag`},
	}

	if _, err := NewJsonRouter(rc, &tagger); err == nil {
		t.Fatal("Expected error for duplicate route keys in routing map, got none")
	}

	// Test case 2: Duplicate keys in drop map
	rc = JsonRouteConfig{
		Route_Key: `action`,
		Route:     []string{`dropme:`, `dropme:`},
	}

	if _, err := NewJsonRouter(rc, &tagger); err == nil {
		t.Fatal("Expected error for duplicate route keys in drop map, got none")
	}

	// Test case 3: Duplicate key across routing map and drop map
	rc = JsonRouteConfig{
		Route_Key: `action`,
		Route:     []string{`duplicate:sometag`, `duplicate:`},
	}

	if _, err := NewJsonRouter(rc, &tagger); err == nil {
		t.Fatal("Expected error for duplicate route key across routing and drop maps, got none")
	}

	// Test case 4: Another order - drop first, then route
	rc = JsonRouteConfig{
		Route_Key: `action`,
		Route:     []string{`duplicate:`, `duplicate:sometag`},
	}

	if _, err := NewJsonRouter(rc, &tagger); err == nil {
		t.Fatal("Expected error for duplicate route key across drop and routing maps, got none")
	}
}

func TestJsonRouterEmptyAndMalformedData(t *testing.T) {
	var tagger testTagger
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}

	rc := JsonRouteConfig{
		Route_Key: `action`,
		Route:     []string{`login:logintag`, `logout:logouttag`},
	}

	jr, err := NewJsonRouter(rc, &tagger)
	if err != nil {
		t.Fatal(err)
	}

	testData := []struct {
		body        string
		description string
		tag         string
		drop        bool
	}{
		{body: ``, description: "empty body", tag: `default`, drop: false},
		{body: `not json at all`, description: "plain text", tag: `default`, drop: false},
		{body: `{invalid json}`, description: "malformed JSON", tag: `default`, drop: false},
		{body: `{"action":}`, description: "incomplete JSON", tag: `default`, drop: false},
		{body: `{"action"`, description: "truncated JSON", tag: `default`, drop: false},
		{body: `[]`, description: "empty array", tag: `default`, drop: false},
		{body: `[1,2,3]`, description: "JSON array", tag: `default`, drop: false},
		{body: `null`, description: "JSON null", tag: `default`, drop: false},
		{body: `123`, description: "JSON number", tag: `default`, drop: false},
		{body: `"string"`, description: "JSON string", tag: `default`, drop: false},
	}

	for i, td := range testData {
		ent := &entry.Entry{
			Tag:  0,
			SRC:  testIP,
			TS:   testTime,
			Data: []byte(td.body),
		}
		if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatalf("Test %d (%s): %v", i, td.description, err)
		} else if td.drop && len(set) != 0 {
			t.Fatalf("Test %d (%s): expected drop, got %d entries", i, td.description, len(set))
		} else if !td.drop && len(set) != 1 {
			t.Fatalf("Test %d (%s): expected 1 entry, got %d", i, td.description, len(set))
		} else if !td.drop {
			if tg, ok := tagger.mp[td.tag]; !ok {
				t.Fatalf("Test %d (%s): tagger didn't create tag %v", i, td.description, td.tag)
			} else if tg != ent.Tag {
				t.Fatalf("Test %d (%s): Invalid tag results: %v != %v", i, td.description, tg, ent.Tag)
			}
		}
	}
}

func TestJsonRouterDropMisses(t *testing.T) {
	var tagger testTagger
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}

	rc := JsonRouteConfig{
		Route_Key:   `action`,
		Route:       []string{`login:logintag`, `logout:logouttag`},
		Drop_Misses: true,
	}

	jr, err := NewJsonRouter(rc, &tagger)
	if err != nil {
		t.Fatal(err)
	}

	testData := []struct {
		body        string
		description string
		drop        bool
	}{
		{body: `{"action":"login"}`, description: "valid login", drop: false},
		{body: `{"action":"logout"}`, description: "valid logout", drop: false},
		{body: `{"action":"unknown"}`, description: "unmatched action", drop: true},
		{body: `{"other":"field"}`, description: "missing route key", drop: true},
		{body: ``, description: "empty body", drop: true},
		{body: `not json`, description: "invalid JSON", drop: true},
		{body: `{malformed}`, description: "malformed JSON", drop: true},
		{body: `[]`, description: "empty array", drop: true},
		{body: `null`, description: "JSON null", drop: true},
		{body: `123`, description: "JSON number", drop: true},
	}

	for i, td := range testData {
		ent := &entry.Entry{
			Tag:  0,
			SRC:  testIP,
			TS:   testTime,
			Data: []byte(td.body),
		}
		if set, err := jr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatalf("Test %d (%s): %v", i, td.description, err)
		} else if td.drop && len(set) != 0 {
			t.Fatalf("Test %d (%s): expected drop, got %d entries", i, td.description, len(set))
		} else if !td.drop && len(set) != 1 {
			t.Fatalf("Test %d (%s): expected 1 entry, got %d", i, td.description, len(set))
		}
	}
}
