/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package dynamic

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gravwell/gcfg"
)

// The mock configs below mirror the shape of the real hosted plugin configs, flattened so
// that no embedded structs are involved yet.  Between them they cover every primitive we
// support: string, int, uint, float, bool, uuid, []string and a named string type, along
// with the json:"-" secrets and the Ingester-UUID that MapRunnerDefinition lifts out.

// mockApi mirrors mimecast.Api, a named string type used as a slice member.
type mockApi string

const (
	mockAudit    mockApi = `audit`
	mockDelivery mockApi = `mta-delivery`
)

// mockOkta mirrors okta.Config: ints and strings with a secret token.
type mockOkta struct {
	Ingester_UUID      string
	Request_Batch_Size int
	Request_Per_Minute int
	Request_Burst      int
	Domain             string
	Token              string `json:"-"`
}

// mockMimecast mirrors mimecast.Config: a named string slice, a plain string slice and
// two secrets.
type mockMimecast struct {
	Ingester_UUID       string
	Tag_Name            string
	Tag_Prefix          string
	Lookback            int
	Requests_Per_Minute int
	Request_Interval    int
	Client_Id           string `json:"-"`
	Client_Secret       string `json:"-"`
	Api                 []mockApi
	Host                string
	Preprocessor        []string
}

// mockJamf mirrors jamf.Config: adds a bool and a []string.
type mockJamf struct {
	Ingester_UUID            string
	Tag_Name                 string
	Host                     string
	Client_Id                string
	Client_Secret            string `json:"-"`
	Page_Size                int
	Sections                 []string
	Insecure_Skip_TLS_Verify bool
}

// mockSQS mirrors sqs.Config: all strings plus a bool.
type mockSQS struct {
	Ingester_UUID     string
	Tag_Name          string
	Queue_URL         string
	Region            string
	Endpoint          string
	Credentials_Type  string
	AKID              string
	Secret            string `json:"-"`
	Ignore_Timestamps bool
}

// mockTester mirrors tester.Config: a duration carried as a string and two bools.
type mockTester struct {
	Ingester_UUID string
	Tag_Name      string
	Interval      string
	Silent        bool
	Test_Errors   bool
}

// mockWiz mirrors wiz.Config, including the unexported members it derives at verify time.
type mockWiz struct {
	Ingester_UUID      string
	Client_Id          string `json:"-"`
	Client_Secret      string `json:"-"`
	Endpoint           string
	Page_Size          int
	Max_Pages_Per_Type int
	Tag_Name           string
	Tag_Override       []string
	Query_Override     []string

	tags    map[string]string
	queries map[string]string
}

// mockWidest is not modelled on a real plugin, it exists to exercise the primitives that
// no current plugin happens to use.
type mockWidest struct {
	Ingester_UUID string
	Name          string
	Count         int
	Size          uint
	Ratio         float64
	Enabled       bool
	Tags          []string
}

// varNames lists the emitted variable names in order, for the shape assertions.
func varNames(c RunnerDefinition) []string {
	out := make([]string, 0, len(c.Variables))
	for _, v := range c.Variables {
		out = append(out, v.Name)
	}
	return out
}

func findVar(c RunnerDefinition, name string) (Variable, bool) {
	for _, v := range c.Variables {
		if v.Name == name {
			return v, true
		}
	}
	return Variable{}, false
}

// TestMapRunnerDefinitionShape checks the variable list MapRunnerDefinition produces for each mock config:
// the names, the declared types, that unexported members are dropped, and that the
// Ingester-UUID is lifted out into RunnerDefinition.UUID rather than left in the list.
func TestMapRunnerDefinitionShape(t *testing.T) {
	for _, tc := range []struct {
		kind  string
		v     any
		names []string
		types map[string]ValueType
	}{
		{
			kind: `okta`, v: mockOkta{},
			names: []string{`Request-Batch-Size`, `Request-Per-Minute`, `Request-Burst`, `Domain`, `Token`},
			types: map[string]ValueType{`Request-Batch-Size`: typeInt, `Domain`: typeString, `Token`: typeString},
		},
		{
			kind: `mimecast`, v: mockMimecast{},
			names: []string{`Tag-Name`, `Tag-Prefix`, `Lookback`, `Requests-Per-Minute`, `Request-Interval`,
				`Client-Id`, `Client-Secret`, `Api`, `Host`, `Preprocessor`},
			types: map[string]ValueType{`Api`: typeSliceString, `Preprocessor`: typeSliceString, `Lookback`: typeInt},
		},
		{
			kind: `jamf`, v: mockJamf{},
			names: []string{`Tag-Name`, `Host`, `Client-Id`, `Client-Secret`, `Page-Size`, `Sections`,
				`Insecure-Skip-TLS-Verify`},
			types: map[string]ValueType{`Insecure-Skip-TLS-Verify`: typeBool, `Sections`: typeSliceString},
		},
		{
			kind: `sqs`, v: mockSQS{},
			names: []string{`Tag-Name`, `Queue-URL`, `Region`, `Endpoint`, `Credentials-Type`, `AKID`,
				`Secret`, `Ignore-Timestamps`},
			types: map[string]ValueType{`Ignore-Timestamps`: typeBool, `AKID`: typeString},
		},
		{
			kind: `tester`, v: mockTester{},
			names: []string{`Tag-Name`, `Interval`, `Silent`, `Test-Errors`},
			types: map[string]ValueType{`Silent`: typeBool, `Interval`: typeString},
		},
		{
			// the unexported tags and queries maps must not appear
			kind: `wiz`, v: mockWiz{},
			names: []string{`Client-Id`, `Client-Secret`, `Endpoint`, `Page-Size`, `Max-Pages-Per-Type`,
				`Tag-Name`, `Tag-Override`, `Query-Override`},
			types: map[string]ValueType{`Tag-Override`: typeSliceString, `Max-Pages-Per-Type`: typeInt},
		},
		{
			kind: `widest`, v: mockWidest{},
			names: []string{`Name`, `Count`, `Size`, `Ratio`, `Enabled`, `Tags`},
			types: map[string]ValueType{`Count`: typeInt, `Size`: typeUint, `Ratio`: typeFloat,
				`Enabled`: typeBool, `Tags`: typeSliceString, `Name`: typeString},
		},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			c, err := MapRunnerDefinition(tc.kind, `prod`, tc.v)
			if err != nil {
				t.Fatal(err)
			}
			if c.Kind != tc.kind || c.Name != `prod` {
				t.Errorf("got kind %q name %q", c.Kind, c.Name)
			}
			if got := varNames(c); !reflect.DeepEqual(got, tc.names) {
				t.Errorf("variables:\n got %q\nwant %q", got, tc.names)
			}
			for name, want := range tc.types {
				v, ok := findVar(c, name)
				if !ok {
					t.Errorf("%s is missing", name)
				} else if v.Type != want {
					t.Errorf("%s has type %s, want %s", name, v.Type, want)
				}
			}
			// a zero struct is a prototype, nothing may carry a value
			for _, v := range c.Variables {
				if v.Value != nil {
					t.Errorf("%s carries a value %v in a zero config", v.Name, v.Value)
				}
			}
		})
	}
}

// TestMapRunnerDefinitionSecrets checks that a json:"-" member is still described so a GUI can ask
// for it, but never carries the value.
func TestMapRunnerDefinitionSecrets(t *testing.T) {
	c, err := MapRunnerDefinition(`okta`, `prod`, mockOkta{
		Domain: `example.okta.com`,
		Token:  `SUPER-SECRET-DO-NOT-LEAK`,
	})
	if err != nil {
		t.Fatal(err)
	}
	tok, ok := findVar(c, `Token`)
	if !ok {
		t.Fatal(`Token was dropped, a GUI could never ask for it`)
	}
	if tok.Type != typeString {
		t.Errorf(`Token type is %s`, tok.Type)
	}
	if tok.Value != nil {
		t.Errorf(`Token leaked its value: %v`, tok.Value)
	}
	if dom, _ := findVar(c, `Domain`); dom.Value != `example.okta.com` {
		t.Errorf(`Domain = %v, want the populated value`, dom.Value)
	}
	// and the secret must not survive into the generated INI either
	ini, err := c.INI()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ini, `SUPER-SECRET-DO-NOT-LEAK`) {
		t.Errorf("the secret leaked into the INI:\n%s", ini)
	}
}

// TestMapRunnerDefinitionUUIDLift checks that Ingester-UUID lands in RunnerDefinition.UUID, stays out of the
// variable list, and is written exactly once by INI.
func TestMapRunnerDefinitionUUIDLift(t *testing.T) {
	c, err := MapRunnerDefinition(`okta`, `prod`, mockOkta{Ingester_UUID: testUUID, Domain: `x`})
	if err != nil {
		t.Fatal(err)
	}
	if c.UUID.String() != testUUID {
		t.Errorf(`RunnerDefinition.UUID = %v, want %s`, c.UUID, testUUID)
	}
	if _, ok := findVar(c, ingesterUUIDName); ok {
		t.Error(`Ingester-UUID is still in the variable list, INI would emit it twice`)
	}
	ini, err := c.INI()
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(ini, ingesterUUIDName); n != 1 {
		t.Errorf("Ingester-UUID appears %d times:\n%s", n, ini)
	}
	// a malformed uuid must be reported rather than quietly dropped
	if _, err = MapRunnerDefinition(`okta`, `prod`, mockOkta{Ingester_UUID: `nope`}); err == nil {
		t.Error(`a malformed Ingester-UUID should be an error`)
	}
}

// TestMapRunnerDefinitionErrors covers the argument checks.
func TestMapRunnerDefinitionErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		kind, cnfg string
		v          any
	}{
		{`empty kind`, ``, `prod`, mockOkta{}},
		{`empty name`, `okta`, ``, mockOkta{}},
	} {
		if _, err := MapRunnerDefinition(tc.kind, tc.cnfg, tc.v); err == nil {
			t.Errorf(`%s: expected an error`, tc.name)
		}
	}
	for _, v := range []any{nil, 42, `a string`, []int{1}} {
		if _, err := MapRunnerDefinition(`k`, `n`, v); err == nil {
			t.Errorf(`%T should not be mappable`, v)
		}
	}
	// an unsupported member type must be reported
	if _, err := MapRunnerDefinition(`k`, `n`, struct{ Bad map[string]string }{}); err == nil {
		t.Error(`a map member should be unsupported`)
	}
}

// TestConfigINIRoundTrip is the end to end check: a populated native config goes through
// MapRunnerDefinition, out through INI, back in through the real gcfg parser, and must land in a
// struct identical to the original apart from the secrets we deliberately withhold.
func TestConfigINIRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		kind string
		in   any
		want any // the original with every json:"-" member zeroed
		into func() any
	}{
		{
			kind: `okta`,
			in: mockOkta{Ingester_UUID: testUUID, Request_Batch_Size: 500, Request_Per_Minute: 60,
				Request_Burst: 10, Domain: `example.okta.com`, Token: `secret-token`},
			want: mockOkta{Ingester_UUID: testUUID, Request_Batch_Size: 500, Request_Per_Minute: 60,
				Request_Burst: 10, Domain: `example.okta.com`},
			into: func() any { return &struct{ Okta map[string]*mockOkta }{} },
		},
		{
			kind: `mimecast`,
			in: mockMimecast{Ingester_UUID: testUUID, Tag_Prefix: `mimecast`, Lookback: 24,
				Requests_Per_Minute: 60, Request_Interval: 30, Client_Id: `pub`, Client_Secret: `shh`,
				Api: []mockApi{mockAudit, mockDelivery}, Host: `api.mimecast.com`,
				Preprocessor: []string{`pp1`, `pp2`}},
			want: mockMimecast{Ingester_UUID: testUUID, Tag_Prefix: `mimecast`, Lookback: 24,
				Requests_Per_Minute: 60, Request_Interval: 30,
				Api: []mockApi{mockAudit, mockDelivery}, Host: `api.mimecast.com`,
				Preprocessor: []string{`pp1`, `pp2`}},
			into: func() any { return &struct{ Mimecast map[string]*mockMimecast }{} },
		},
		{
			kind: `jamf`,
			in: mockJamf{Ingester_UUID: testUUID, Tag_Name: `jamf`, Host: `jamf.example.com`,
				Client_Id: `cid`, Client_Secret: `shh`, Page_Size: 100,
				Sections: []string{`GENERAL`, `HARDWARE`}, Insecure_Skip_TLS_Verify: true},
			want: mockJamf{Ingester_UUID: testUUID, Tag_Name: `jamf`, Host: `jamf.example.com`,
				Client_Id: `cid`, Page_Size: 100,
				Sections: []string{`GENERAL`, `HARDWARE`}, Insecure_Skip_TLS_Verify: true},
			into: func() any { return &struct{ Jamf map[string]*mockJamf }{} },
		},
		{
			kind: `sqs`,
			in: mockSQS{Ingester_UUID: testUUID, Tag_Name: `sqs`, Queue_URL: `https://sqs.us-east-1.amazonaws.com/1/q`,
				Region: `us-east-1`, Credentials_Type: `static`, AKID: `AKIAEXAMPLE`, Secret: `shh`,
				Ignore_Timestamps: true},
			want: mockSQS{Ingester_UUID: testUUID, Tag_Name: `sqs`, Queue_URL: `https://sqs.us-east-1.amazonaws.com/1/q`,
				Region: `us-east-1`, Credentials_Type: `static`, AKID: `AKIAEXAMPLE`,
				Ignore_Timestamps: true},
			into: func() any { return &struct{ SQS map[string]*mockSQS }{} },
		},
		{
			kind: `tester`,
			in: mockTester{Ingester_UUID: testUUID, Tag_Name: `test`, Interval: `1s`,
				Silent: true, Test_Errors: true},
			want: mockTester{Ingester_UUID: testUUID, Tag_Name: `test`, Interval: `1s`,
				Silent: true, Test_Errors: true},
			into: func() any { return &struct{ Tester map[string]*mockTester }{} },
		},
		{
			kind: `wiz`,
			in: mockWiz{Ingester_UUID: testUUID, Client_Id: `cid`, Client_Secret: `shh`,
				Endpoint: `https://api.us1.app.wiz.io/graphql`, Page_Size: 100, Max_Pages_Per_Type: 5,
				Tag_Name: `wiz`, Tag_Override: []string{`a:b`}, Query_Override: []string{`a:/tmp/q.graphql`}},
			want: mockWiz{Ingester_UUID: testUUID,
				Endpoint: `https://api.us1.app.wiz.io/graphql`, Page_Size: 100, Max_Pages_Per_Type: 5,
				Tag_Name: `wiz`, Tag_Override: []string{`a:b`}, Query_Override: []string{`a:/tmp/q.graphql`}},
			into: func() any { return &struct{ Wiz map[string]*mockWiz }{} },
		},
		{
			// every primitive at once, including the uint and float no plugin uses
			kind: `widest`,
			in: mockWidest{Ingester_UUID: testUUID, Name: `all the types`, Count: -17, Size: 1 << 40,
				Ratio: 3.14159265358979, Enabled: true, Tags: []string{`a`, `b`}},
			want: mockWidest{Ingester_UUID: testUUID, Name: `all the types`, Count: -17, Size: 1 << 40,
				Ratio: 3.14159265358979, Enabled: true, Tags: []string{`a`, `b`}},
			into: func() any { return &struct{ Widest map[string]*mockWidest }{} },
		},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			c, err := MapRunnerDefinition(tc.kind, `prod`, tc.in)
			if err != nil {
				t.Fatalf(`MapRunnerDefinition: %v`, err)
			}
			ini, err := c.INI()
			if err != nil {
				t.Fatalf(`INI: %v`, err)
			}
			t.Logf("generated:\n%s", ini)

			tgt := tc.into()
			if err = gcfg.ReadStringInto(tgt, ini); err != nil {
				t.Fatalf("gcfg rejected the generated INI: %v\n%s", err, ini)
			}
			// pull the single subsection back out of the map
			m := reflect.ValueOf(tgt).Elem().Field(0)
			got := m.MapIndex(reflect.ValueOf(`prod`))
			if !got.IsValid() {
				t.Fatalf("subsection \"prod\" is missing, gcfg produced %v", m)
			}
			if have := got.Elem().Interface(); !reflect.DeepEqual(have, tc.want) {
				t.Errorf("round trip mismatch\n got %+v\nwant %+v", have, tc.want)
			}
		})
	}
}

// mockPointers checks that pointer members are followed, and that a nil pointer is
// described without a value rather than panicking.
type mockPointers struct {
	Name    *string
	Count   *int
	Enabled *bool
	Missing *string
	Renamed string `gcfg:"custom-name"`
}

func TestMapRunnerDefinitionPointersAndTags(t *testing.T) {
	name, count, enabled := `pointed at`, 7, true
	c, err := MapRunnerDefinition(`ptr`, `prod`, &mockPointers{
		Name: &name, Count: &count, Enabled: &enabled, Renamed: `x`,
	})
	if err != nil {
		t.Fatal(err)
	}
	// a gcfg ident override wins over the Go member name
	if got := varNames(c); !reflect.DeepEqual(got, []string{`Name`, `Count`, `Enabled`, `Missing`, `custom-name`}) {
		t.Errorf(`variables = %q`, got)
	}
	for name, want := range map[string]ValueType{
		`Name`: typeString, `Count`: typeInt, `Enabled`: typeBool, `Missing`: typeString,
	} {
		if v, ok := findVar(c, name); !ok || v.Type != want {
			t.Errorf(`%s: type %s ok=%v, want %s`, name, v.Type, ok, want)
		}
	}
	// the nil pointer is described but carries nothing
	if v, _ := findVar(c, `Missing`); v.Value != nil {
		t.Errorf(`nil pointer produced a value %v`, v.Value)
	}
	// and the ones that do point somewhere carry the pointed at value
	if v, _ := findVar(c, `Name`); v.Value != `pointed at` {
		t.Errorf(`Name = %v`, v.Value)
	}
	if v, _ := findVar(c, `Count`); v.Value != int64(7) {
		t.Errorf(`Count = %v (%T)`, v.Value, v.Value)
	}

	ini, err := c.INI()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ini, `Missing`) {
		t.Errorf("an unset member reached the INI:\n%s", ini)
	}
	var tgt struct {
		Ptr map[string]*struct {
			Name    string
			Count   int
			Enabled bool
			Renamed string `gcfg:"custom-name"`
		}
	}
	if err = gcfg.ReadStringInto(&tgt, ini); err != nil {
		t.Fatalf("gcfg rejected %v\n%s", err, ini)
	}
	got := tgt.Ptr[`prod`]
	if got.Name != name || got.Count != count || got.Enabled != enabled || got.Renamed != `x` {
		t.Errorf(`round tripped to %+v`, got)
	}
}

// TestINIErrors covers the guards on INI itself.
func TestINIErrors(t *testing.T) {
	for _, c := range []RunnerDefinition{{Name: `prod`}, {Kind: `okta`}} {
		if _, err := c.INI(); err == nil {
			t.Errorf(`%+v should not produce an INI`, c)
		}
	}
	// a variable that cannot be represented must fail the whole config rather than
	// silently emitting a broken file
	c := RunnerDefinition{Kind: `k`, Name: `n`, Variables: []Variable{
		{Name: `Bad`, Type: typeString, Value: "a`b\x01c"},
	}}
	if _, err := c.INI(); err == nil {
		t.Error(`an unrepresentable value should fail INI`)
	}
}
