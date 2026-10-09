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
	"reflect"
	"strings"
	"testing"

	"github.com/gravwell/gcfg"
)

// These mirror the shared bases in the hosted package.  gcfg promotes an embedded struct
// into the parent INI section rather than giving it a subsection, so MapRunnerDefinition has to
// flatten them the same way.

type mockBase struct {
	Ingester_UUID string
}

type mockSingleTag struct {
	Tag_Name string
}

type mockMultiTag struct {
	Tag_Name   string
	Tag_Prefix string
}

type mockPolling struct {
	Lookback            int
	Requests_Per_Minute int
	Request_Interval    int
}

// mockEmbeddedMimecast mirrors the real mimecast.Config, embeddings and all.
type mockEmbeddedMimecast struct {
	mockBase
	mockMultiTag
	mockPolling
	Client_Id     string `json:"-"`
	Client_Secret string `json:"-"`
	Api           []mockApi
	Host          string
	Preprocessor  []string
}

// mockEmbeddedJamf mirrors the real jamf.Config.
type mockEmbeddedJamf struct {
	mockBase
	mockSingleTag
	mockPolling

	Host          string
	Client_Id     string
	Client_Secret string `json:"-"`
	Page_Size     int
	Sections      []string

	Insecure_Skip_TLS_Verify bool
}

// TestEmbeddedFlattening checks that promoted members land in the parent list, in the
// position where the embedded struct was declared, and that nothing gains a nested shape.
func TestEmbeddedFlattening(t *testing.T) {
	for _, tc := range []struct {
		name  string
		v     any
		names []string
	}{
		{
			name: `mimecast`, v: mockEmbeddedMimecast{},
			// Ingester-UUID is lifted out, the rest keep declaration order with the
			// embedded members spliced in where the embedding appears
			names: []string{`Tag-Name`, `Tag-Prefix`, `Lookback`, `Requests-Per-Minute`,
				`Request-Interval`, `Client-Id`, `Client-Secret`, `Api`, `Host`, `Preprocessor`},
		},
		{
			name: `jamf`, v: mockEmbeddedJamf{},
			names: []string{`Tag-Name`, `Lookback`, `Requests-Per-Minute`, `Request-Interval`,
				`Host`, `Client-Id`, `Client-Secret`, `Page-Size`, `Sections`,
				`Insecure-Skip-TLS-Verify`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := MapRunnerDefinition(tc.name, `prod`, tc.v)
			if err != nil {
				t.Fatal(err)
			}
			if got := varNames(c); !reflect.DeepEqual(got, tc.names) {
				t.Errorf("variables:\n got %q\nwant %q", got, tc.names)
			}
			// flattening means no member may be described as a struct
			for _, v := range c.Variables {
				if v.Type.Complex() {
					t.Errorf(`%s came out as %s, embedded members must be flattened`, v.Name, v.Type)
				}
			}
		})
	}
}

// TestEmbeddedMatchesFlat checks that embedding produces exactly the same description as
// writing the same members out by hand, which is the whole point of flattening.
func TestEmbeddedMatchesFlat(t *testing.T) {
	embedded, err := MapRunnerDefinition(`mimecast`, `prod`, mockEmbeddedMimecast{
		Ingester_UUID: testUUID,
		Tag_Prefix:    `mimecast`,
		Lookback:      24, Requests_Per_Minute: 60, Request_Interval: 30,
		Client_Id: `pub`, Client_Secret: `shh`,
		Api:  []mockApi{mockAudit, mockDelivery},
		Host: `api.mimecast.com`, Preprocessor: []string{`pp1`, `pp2`},
	})
	if err != nil {
		t.Fatal(err)
	}
	flat, err := MapRunnerDefinition(`mimecast`, `prod`, mockMimecast{
		Ingester_UUID: testUUID, Tag_Prefix: `mimecast`,
		Lookback: 24, Requests_Per_Minute: 60, Request_Interval: 30,
		Client_Id: `pub`, Client_Secret: `shh`,
		Api:  []mockApi{mockAudit, mockDelivery},
		Host: `api.mimecast.com`, Preprocessor: []string{`pp1`, `pp2`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(embedded, flat) {
		t.Errorf("embedded and flat configs differ\n embedded %+v\n     flat %+v", embedded, flat)
	}
	ei, err := embedded.INI()
	if err != nil {
		t.Fatal(err)
	}
	fi, err := flat.INI()
	if err != nil {
		t.Fatal(err)
	}
	if ei != fi {
		t.Errorf("generated INI differs\nembedded:\n%s\nflat:\n%s", ei, fi)
	}
}

// TestEmbeddedRoundTrip drives an embedded config all the way back through gcfg.  gcfg
// promotes the embedded members too, so the emitted flat keys must land in them.
func TestEmbeddedRoundTrip(t *testing.T) {
	in := mockEmbeddedJamf{
		Ingester_UUID: testUUID,
		Tag_Name:      `jamf`,
		Lookback:      1, Requests_Per_Minute: 60, Request_Interval: 600,
		Host:      `https://yourserver.jamfcloud.com`,
		Client_Id: `api-client-id`, Client_Secret: `shh`,
		Page_Size: 100, Sections: []string{`GENERAL`, `DISK_ENCRYPTION`},
		Insecure_Skip_TLS_Verify: true,
	}
	want := in
	want.Client_Secret = `` // withheld by design

	c, err := MapRunnerDefinition(`jamf`, `prod`, in)
	if err != nil {
		t.Fatal(err)
	}
	ini, err := c.INI()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("generated:\n%s", ini)

	var tgt struct {
		Jamf map[string]*mockEmbeddedJamf
	}
	if err = gcfg.ReadStringInto(&tgt, ini); err != nil {
		t.Fatalf("gcfg rejected the generated INI: %v\n%s", err, ini)
	}
	got := tgt.Jamf[`prod`]
	if got == nil {
		t.Fatal(`subsection "prod" is missing`)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("round trip mismatch\n got %+v\nwant %+v", *got, want)
	}
}

// shadowOuter declares Tag-Name directly while also embedding a struct that carries it.
// Go promotion gives the outer member priority and so does gcfg, so only one variable may
// be produced and it has to be the outer one.
type shadowOuter struct {
	mockSingleTag
	Tag_Name string
	Host     string
}

// shadowNested puts the shadowing one level down to check that depth, not order, decides.
type shadowMiddle struct {
	mockSingleTag
	Tag_Name string
}

type shadowNested struct {
	shadowMiddle
	Host string
}

// shadowAmbiguous embeds two structs that both carry Tag-Name at the same depth.  Neither
// Go nor gcfg can address that member, so it must be reported rather than guessed at.
type shadowAmbiguous struct {
	mockSingleTag
	mockMultiTag
	Host string
}

func TestEmbeddedShadowing(t *testing.T) {
	c, err := MapRunnerDefinition(`shadow`, `prod`, shadowOuter{
		mockSingleTag: mockSingleTag{Tag_Name: `inner`},
		Tag_Name:      `outer`,
		Host:          `h`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := varNames(c); !reflect.DeepEqual(got, []string{`Tag-Name`, `Host`}) {
		t.Fatalf(`variables = %q, want a single Tag-Name`, got)
	}
	if v, _ := findVar(c, `Tag-Name`); v.Value != `outer` {
		t.Errorf(`Tag-Name = %v, want the outer member's value`, v.Value)
	}
	// the emitted key must land back on the outer member, which is the one gcfg picks
	ini, err := c.INI()
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(ini, `Tag-Name`); n != 1 {
		t.Errorf("Tag-Name emitted %d times:\n%s", n, ini)
	}
	var tgt struct {
		Shadow map[string]*shadowOuter
	}
	if err = gcfg.ReadStringInto(&tgt, ini); err != nil {
		t.Fatalf("gcfg rejected %v\n%s", err, ini)
	}
	if got := tgt.Shadow[`prod`]; got.Tag_Name != `outer` || got.mockSingleTag.Tag_Name != `` {
		t.Errorf(`gcfg landed the value on %+v, want it on the outer member only`, got)
	}
}

func TestEmbeddedShadowingNested(t *testing.T) {
	c, err := MapRunnerDefinition(`shadow`, `prod`, shadowNested{
		mockSingleTag: mockSingleTag{Tag_Name: `deep`},
		Tag_Name:      `middle`,
		Host:          `h`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := varNames(c); !reflect.DeepEqual(got, []string{`Tag-Name`, `Host`}) {
		t.Fatalf(`variables = %q`, got)
	}
	if v, _ := findVar(c, `Tag-Name`); v.Value != `middle` {
		t.Errorf(`Tag-Name = %v, want the shallower member's value`, v.Value)
	}
}

func TestEmbeddedAmbiguous(t *testing.T) {
	_, err := MapRunnerDefinition(`shadow`, `prod`, shadowAmbiguous{})
	if err == nil {
		t.Fatal(`two members promoted from the same depth should be reported`)
	}
	if !errors.Is(err, ErrAmbiguousMember) {
		t.Errorf(`error does not wrap ErrAmbiguousMember: %v`, err)
	}
	if !strings.Contains(err.Error(), `Tag-Name`) {
		t.Errorf(`error should name the member: %v`, err)
	}
}

// TestEmbeddedPointer checks that an embedded pointer is followed, and that a nil one
// still contributes its members as unset rather than panicking.
func TestEmbeddedPointer(t *testing.T) {
	type withPtr struct {
		*mockPolling
		Host string
	}
	c, err := MapRunnerDefinition(`ptr`, `prod`, withPtr{Host: `h`})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`Lookback`, `Requests-Per-Minute`, `Request-Interval`, `Host`}
	if got := varNames(c); !reflect.DeepEqual(got, want) {
		t.Fatalf("variables = %q, want %q", got, want)
	}
	for _, n := range []string{`Lookback`, `Requests-Per-Minute`, `Request-Interval`} {
		if v, _ := findVar(c, n); v.Value != nil {
			t.Errorf(`%s carries %v from a nil embedded pointer`, n, v.Value)
		}
	}
	// and a populated one contributes values
	c, err = MapRunnerDefinition(`ptr`, `prod`, withPtr{mockPolling: &mockPolling{Lookback: 24}, Host: `h`})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := findVar(c, `Lookback`); v.Value != int64(24) {
		t.Errorf(`Lookback = %v (%T)`, v.Value, v.Value)
	}
}

// TestEmbeddedDeep checks the recursion guard fires rather than running away.
func TestEmbeddedDeep(t *testing.T) {
	type l8 struct{ Deep string }
	type l7 struct{ l8 }
	type l6 struct{ l7 }
	type l5 struct{ l6 }
	type l4 struct{ l5 }
	type l3 struct{ l4 }
	type l2 struct{ l3 }
	type l1 struct{ l2 }
	type l0 struct{ l1 }
	// ten levels of embedding is past maxStructDepth
	type tooDeep struct{ l0 }
	if _, err := MapRunnerDefinition(`deep`, `prod`, tooDeep{}); err == nil {
		t.Error(`excessive nesting should be reported`)
	}
	// but a shallow chain still flattens all the way down
	c, err := MapRunnerDefinition(`deep`, `prod`, l5{})
	if err != nil {
		t.Fatal(err)
	}
	if got := varNames(c); !reflect.DeepEqual(got, []string{`Deep`}) {
		t.Errorf(`variables = %q, want the promoted Deep`, got)
	}
}
