/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package dynamic

import (
	"encoding/json"
	"errors"
	"maps"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
	"uuid"

	"github.com/gravwell/gcfg"
)

// metaConfig is a minimal plugin config used to prove that a block carrying metadata
// comments still loads through the real gcfg parser.
type metaConfig struct {
	Ingester_UUID string
	Tag_Name      string
	Token         string `json:"-"`
	Preprocessor  []string
}

// nastyValues is the set of strings a metadata key or value has to survive.  Every one of
// them means something to somebody in the path: a quote and a backslash to the quoting
// itself, a semicolon and a hash to the comment scanner, a bracket to the section reader,
// a newline to anything reading the file a line at a time, a control character to the
// value escaper that refuses them outright, and a lone 0xff to anything assuming UTF-8.
var nastyValues = map[string]string{
	`plain`:              `an ordinary value`,
	`quote`:              `he said "hello" loudly`,
	`backslash`:          `C:\path\to\thing`,
	`backslash-quote`:    `trailing pair \"`,
	`double-backslash`:   `a\\b`,
	`backtick`:           "raw `string` delimiter",
	`newline`:            "first\nsecond\nthird",
	`carriage-return`:    "wrapped\r\nline",
	`tab`:                "col1\tcol2",
	`semicolon`:          `; not a comment`,
	`hash`:               `# also not a comment`,
	`brackets`:           `[section "name"]`,
	`equals`:             `key=value=still`,
	`control`:            "bell\a null\x00 escape\x1b",
	`del`:                "high\x7fbyte",
	`invalid-utf8`:       "bad\xffbyte",
	`unicode`:            "héllo wörld ✓ 🔐",
	`line-separator`:     "a\u2028b\u2029c",
	`bidi-override`:      "a\u202eb",
	`zero-width`:         "a\u200bb\ufeffc",
	`astral-unprintable`: "a\U000e0001b",
	`empty`:              ``,
	`spaces`:             `   leading and trailing   `,
	`only-quotes`:        `""""`,
	`looks-like-a-pair`:  `"k"="v"`,
	`looks-like-comment`: "#gravwell-metadata \"Forged\" \"a\"=\"b\"",
	``:                   `the key itself is empty`,
	`"quoted-key"`:       `the key carries quotes`,
	"key\nwith\nnewline": `the key carries newlines`,
	`key\with\backslash`: `the key carries backslashes`,
}

// TestMetadataCommentRoundTrip is the core claim: whatever a map holds, the comment it is
// written to hands back exactly that map and nothing else.
func TestMetadataCommentRoundTrip(t *testing.T) {
	for k, v := range nastyValues {
		single := Variable{Name: `Token`, Type: typeSecret, Value: `shh`,
			Metadata: map[string]string{k: v}}
		got := roundTripMetadata(t, single)
		if !maps.Equal(got, single.Metadata) {
			t.Errorf("key %q value %q round tripped to %#v", k, v, got)
		}
	}
	// and all of them at once, so that no pair can be read as part of its neighbor
	all := Variable{Name: `Token`, Type: typeSecret, Value: `shh`, Metadata: nastyValues}
	if got := roundTripMetadata(t, all); !maps.Equal(got, nastyValues) {
		t.Errorf("the full set round tripped to %#v", got)
	}
}

// TestMetadataCommentIsPrintable is the rule the encoding exists to keep: a comment is a
// line, and nothing in it may be anything but printable text.  A newline would end the
// comment outright, and a carriage return, a form feed or a bidi override would leave a
// line that renders as something other than what it says.
func TestMetadataCommentIsPrintable(t *testing.T) {
	line := (Variable{Name: "Token\nSmuggled", Metadata: nastyValues}).metadataComment()
	for i, r := range line {
		if r == utf8.RuneError {
			// a lone byte, which only shows up here if it was written rather than escaped
			if _, size := utf8.DecodeRuneInString(line[i:]); size == 1 {
				t.Fatalf("byte %d of the comment is not valid UTF-8: %q", i, line)
			}
		}
		if !strconv.IsPrint(r) {
			t.Fatalf("byte %d of the comment is the unprintable %q: %q", i, r, line)
		}
	}
	// and the escapes are the numeric ones, not the literals
	for _, want := range []string{`\x0a`, `\x09`, `\x0d`, `\x00`, `\x1b`, `\x7f`, `\xff`, `\u2028`, `\u202e`, `\U000e0001`} {
		if !strings.Contains(line, want) {
			t.Errorf("the comment does not carry a %s escape: %q", want, line)
		}
	}
	// the name is quoted by the same encoder, a variable cannot smuggle a line either
	if !strings.Contains(line, `"Token\x0aSmuggled"`) {
		t.Errorf("the variable name was not escaped: %q", line)
	}
}

// roundTripMetadata renders a variable's metadata comment and reads it back.
func roundTripMetadata(t *testing.T, v Variable) map[string]string {
	t.Helper()
	line := v.metadataComment()
	if strings.ContainsAny(line, "\n\r") {
		t.Fatalf("the comment spans more than one line: %q", line)
	}
	md, err := ParseINIMetadata(line)
	if err != nil {
		t.Fatalf("parsing %q failed: %v", line, err)
	}
	return md[v.Name]
}

// TestMetadataSurvivesTheWholeBlock is the trip the feature exists for: a definition is
// rendered to a config file, the file is read by the parser that actually reads these,
// and the metadata comes back onto a definition that was mapped from the loaded config
// and so has no metadata of its own.
func TestMetadataSurvivesTheWholeBlock(t *testing.T) {
	c := RunnerDefinition{Kind: `metatest`, Name: `prod`, Variables: []Variable{
		{Name: `Tag-Name`, Type: typeString, Value: `metatest`,
			Metadata: map[string]string{`source`: `default`, `note`: "line\nbreak"}},
		{Name: `Token`, Type: typeSecret, Value: `shh`,
			Metadata: map[string]string{`secret-key`: `prod/metatest`, `quoted`: `a "b" \c`}},
		{Name: `Preprocessor`, Type: typeSliceString, Value: []string{`pp1`, `pp2`},
			Metadata: map[string]string{`order`: `1;2`}},
		// an unset member still gets to keep its note
		{Name: `Missing`, Type: typeString, Metadata: map[string]string{`why`: `unset on purpose`}},
		// and a member with no metadata contributes no comment
		{Name: `Ingester-UUID`, Type: typeString, Value: testUUID},
	}}
	ini, err := c.INI()
	if err != nil {
		t.Fatalf("INI failed: %v", err)
	}
	t.Logf("generated:\n%s", ini)

	// the parser that reads these in production has to be untroubled by every one of them
	var tgt struct {
		Metatest map[string]*metaConfig
	}
	if err = gcfg.ReadStringInto(&tgt, ini); err != nil {
		t.Fatalf("gcfg rejected the generated INI: %v\n%s", err, ini)
	}
	if got := tgt.Metatest[`prod`]; got == nil {
		t.Fatal("the section did not load")
	} else if got.Tag_Name != `metatest` || got.Token != `shh` {
		t.Errorf("the values did not survive: %+v", got)
	}
	// an unset member must not have been conjured into existence by its comment
	if n := strings.Count(ini, "\tMissing="); n != 0 {
		t.Errorf("an unset member reached the INI:\n%s", ini)
	}

	// the definition as it comes back from the loaded config: same variables, no metadata
	back := RunnerDefinition{Kind: c.Kind, Name: c.Name}
	for _, v := range c.Variables {
		v.Metadata = nil
		back.Variables = append(back.Variables, v)
	}
	if err = back.RestoreMetadata(ini); err != nil {
		t.Fatalf("RestoreMetadata failed: %v", err)
	}
	for i, want := range c.Variables {
		if got := back.Variables[i].Metadata; !maps.Equal(got, want.Metadata) {
			t.Errorf("%s came back with %#v, wanted %#v", want.Name, got, want.Metadata)
		}
	}
}

// TestMetadataRenderingIsStable covers the map iteration order.  The client compares a
// rendered block against what is on disk to decide whether to rewrite it, so a rendering
// that shuffled itself would rewrite the file, and restart the runner, on every poll.
func TestMetadataRenderingIsStable(t *testing.T) {
	c := RunnerDefinition{Kind: `metatest`, Name: `prod`, Variables: []Variable{
		{Name: `Token`, Type: typeSecret, Value: `shh`, Metadata: nastyValues},
	}}
	first, err := c.INI()
	if err != nil {
		t.Fatalf("INI failed: %v", err)
	}
	for i := range 32 {
		next, err := c.INI()
		if err != nil {
			t.Fatalf("INI failed: %v", err)
		} else if next != first {
			t.Fatalf("rendering %d differs:\n%s\n---\n%s", i, first, next)
		}
	}
}

// TestValueCannotForgeMetadata covers a value trying to write structure into the block it
// sits in.  There are two ways it can try and the block has to survive both.
func TestValueCannotForgeMetadata(t *testing.T) {
	// a forged metadata comment needs quotes, and a quote puts the whole value on the
	// escaped path, so the forgery arrives as one line of text inside one value
	quoted := "harmless\n\t#gravwell-metadata \"Tag-Name\" \"secret-key\"=\"attacker/owned\"\n" +
		"\t;gravwell-metadata \"Token\" \"secret-key\"=\"attacker/owned\"\nstill harmless"
	// a value carrying no quote, backslash or backtick stays on the raw path, where it
	// really does span lines: these forged lines are lines of the file, and the only
	// thing that keeps them from being read as structure is the scanner knowing it is
	// inside a string
	raw := "harmless\n[metatest \u0060other\u0060]\n#gravwell-metadata \u0060X\u0060\nstill harmless"
	raw = strings.ReplaceAll(raw, "\u0060", ``) // no backtick, or the value is not raw

	c := RunnerDefinition{Kind: `metatest`, Name: `prod`, Variables: []Variable{
		{Name: `Tag-Name`, Type: typeString, Value: `metatest`,
			Metadata: map[string]string{`source`: `default`}},
		{Name: `Token`, Type: typeSecret, Value: quoted},
		{Name: `Preprocessor`, Type: typeSliceString, Value: []string{raw}},
	}}
	ini, err := c.INI()
	if err != nil {
		t.Fatalf("INI failed: %v", err)
	}
	t.Logf("generated:\n%s", ini)

	// the raw value has to actually span lines, or this test proves nothing
	if !strings.Contains(ini, "\nstill harmless`") {
		t.Fatalf("the raw value did not span lines, the scanner is not being exercised:\n%s", ini)
	}
	// the forged section header is one of those lines, and must not be read as one
	if !strings.Contains(ini, "\n[metatest other]\n") {
		t.Fatalf("the forged section header is not in the block:\n%s", ini)
	}

	md, err := ParseINIMetadata(ini)
	if err != nil {
		t.Fatalf("ParseINIMetadata failed: %v", err)
	}
	if len(md) != 1 {
		t.Fatalf("a value forged metadata, got %#v from:\n%s", md, ini)
	}
	if want := (map[string]string{`source`: `default`}); !maps.Equal(md[`Tag-Name`], want) {
		t.Errorf("Tag-Name came back with %#v", md[`Tag-Name`])
	}

	// and both values still have to survive, none of this may mangle them
	var tgt struct {
		Metatest map[string]*metaConfig
	}
	if err = gcfg.ReadStringInto(&tgt, ini); err != nil {
		t.Fatalf("gcfg rejected the generated INI: %v\n%s", err, ini)
	}
	got := tgt.Metatest[`prod`]
	if got == nil {
		t.Fatal("the section did not load")
	} else if got.Token != quoted {
		t.Errorf("the escaped value came back as %q", got.Token)
	} else if len(got.Preprocessor) != 1 || got.Preprocessor[0] != raw {
		t.Errorf("the raw value came back as %q", got.Preprocessor)
	}
	// the forged header must not have opened a section of its own
	if _, forged := tgt.Metatest[`other`]; forged {
		t.Error("a value opened a section")
	}
}

// TestParseINIMetadataIgnoresProse covers an operator writing about this rather than an
// instance of it.  A comment that cannot be parsed fails the whole blob, so a sentence
// mistaken for a mangled metadata comment would throw away every real one in the file,
// which is a great deal more than the sentence is worth.
func TestParseINIMetadataIgnoresProse(t *testing.T) {
	for _, prose := range []string{
		`# gravwell-metadata notes follow, do not edit them by hand`,
		`#gravwell-metadata comments are written by the webserver`,
		`; gravwell-metadata is how a secret key is remembered`,
		`# gravwell-metadata-format is described in the docs`,
		`#gravwell-metadata` + "\tsee the docs",
	} {
		blob := "[metatest \"prod\"]\n\t" + prose + "\n" +
			"\t#gravwell-metadata \"Token\" \"secret-key\"=\"prod/metatest\"\n\tToken=`shh`\n"
		md, err := ParseINIMetadata(blob)
		if err != nil {
			t.Errorf("prose %q failed the blob: %v", prose, err)
			continue
		}
		want := map[string]string{`secret-key`: `prod/metatest`}
		if !maps.Equal(md[`Token`], want) {
			t.Errorf("prose %q cost the real comment, got %#v", prose, md)
		}
	}
}

// TestParseINIMetadataIgnoresOtherComments covers the comments a config file is entitled
// to carry: our own remote marker, anything an operator wrote, and a comment that merely
// mentions the marker rather than being one.
func TestParseINIMetadataIgnoresOtherComments(t *testing.T) {
	const blob = "" +
		remoteMarker + "\n" +
		"; an operator's note about the section below\n" +
		"# gravwell-metadata-format is described in the docs\n" +
		"; gravwell-metadatas are not a thing\n" +
		"[metatest \"prod\"]\n" +
		"\t; nothing to see here\n" +
		"\tTag-Name=`metatest`\n" +
		"\tToken=\"#gravwell-metadata \\\"Forged\\\" \\\"a\\\"=\\\"b\\\"\"\n"
	md, err := ParseINIMetadata(blob)
	if err != nil {
		t.Fatalf("ParseINIMetadata failed: %v", err)
	} else if len(md) != 0 {
		t.Errorf("ordinary comments parsed as metadata: %#v", md)
	}
}

// TestParseINIMetadataAcceptsHandWriting covers the shapes a person would reasonably type
// for something this package normally writes itself: a hash instead of a semicolon, and
// space after the comment character.
func TestParseINIMetadataAcceptsHandWriting(t *testing.T) {
	for _, blob := range []string{
		"[metatest \"prod\"]\n\t#gravwell-metadata \"Token\" \"secret-key\"=\"prod/metatest\"\n",
		"[metatest \"prod\"]\n\t;gravwell-metadata \"Token\" \"secret-key\"=\"prod/metatest\"\n",
		"[metatest \"prod\"]\n  ;  gravwell-metadata   \"Token\"   \"secret-key\"=\"prod/metatest\"  \n",
	} {
		md, err := ParseINIMetadata(blob)
		if err != nil {
			t.Fatalf("ParseINIMetadata(%q) failed: %v", blob, err)
		}
		want := map[string]string{`secret-key`: `prod/metatest`}
		if !maps.Equal(md[`Token`], want) {
			t.Errorf("%q parsed as %#v", blob, md)
		}
	}
}

// TestParseINIMetadataErrors covers the comments that open with the marker and then do
// not deliver.  Each one is a value that was meant to be restored and cannot be, so each
// one is an error rather than a comment quietly skipped.
func TestParseINIMetadataErrors(t *testing.T) {
	for name, blob := range map[string]string{
		`no payload`:        "#gravwell-metadata",
		`unterminated name`: "#gravwell-metadata \"Token",
		`no pairs`:          "#gravwell-metadata \"Token\"",
		`unquoted key`:      "#gravwell-metadata \"Token\" k=\"v\"",
		`no equals`:         "#gravwell-metadata \"Token\" \"k\" \"v\"",
		`no value`:          "#gravwell-metadata \"Token\" \"k\"=",
		`unquoted value`:    "#gravwell-metadata \"Token\" \"k\"=v",
		`bad escape`:        "#gravwell-metadata \"Token\" \"k\"=\"\\q\"",
		`trailing junk`:     "#gravwell-metadata \"Token\" \"k\"=\"v\"junk",
		`duplicate key`:     "#gravwell-metadata \"Token\" \"k\"=\"v\" \"k\"=\"w\"",
		`duplicate name`:    "#gravwell-metadata \"Token\" \"k\"=\"v\"\n#gravwell-metadata \"Token\" \"j\"=\"w\"",
		`two sections`: "[metatest \"a\"]\n#gravwell-metadata \"Token\" \"k\"=\"v\"\n" +
			"[metatest \"b\"]\n#gravwell-metadata \"Other\" \"k\"=\"v\"\n",
	} {
		md, err := ParseINIMetadata(blob)
		if err == nil {
			t.Errorf("%s: %q parsed as %#v", name, blob, md)
		} else if !errors.Is(err, ErrBadMetadataComment) {
			t.Errorf("%s: %q failed with %v, which does not wrap ErrBadMetadataComment", name, blob, err)
		} else if md != nil {
			t.Errorf("%s: a failed parse handed back %#v", name, md)
		}
	}
}

// TestRestoreMetadataLeavesUnknownVariablesAlone covers a plugin that renamed or retired a
// member: the note about it has nowhere to go, and a note with nowhere to go is not a
// reason to refuse an otherwise loadable config.
func TestRestoreMetadataLeavesUnknownVariablesAlone(t *testing.T) {
	const blob = "[metatest \"prod\"]\n" +
		"\t#gravwell-metadata \"Retired\" \"secret-key\"=\"old\"\n" +
		"\t#gravwell-metadata \"Token\" \"secret-key\"=\"prod/metatest\"\n" +
		"\tToken=`shh`\n"
	c := RunnerDefinition{Kind: `metatest`, Name: `prod`, Variables: []Variable{
		{Name: `Tag-Name`, Type: typeString, Value: `metatest`},
		{Name: `Token`, Type: typeSecret, Value: `shh`},
	}}
	if err := c.RestoreMetadata(blob); err != nil {
		t.Fatalf("RestoreMetadata failed: %v", err)
	}
	if c.Variables[0].Metadata != nil {
		t.Errorf("a variable with no metadata came back with %#v", c.Variables[0].Metadata)
	}
	if want := (map[string]string{`secret-key`: `prod/metatest`}); !maps.Equal(c.Variables[1].Metadata, want) {
		t.Errorf("Token came back with %#v", c.Variables[1].Metadata)
	}
}

// TestRestoreMetadataOnABlobWithNone leaves what it found alone: a config file written
// before any of this existed says nothing about metadata, which is not the same as saying
// there is none.
func TestRestoreMetadataOnABlobWithNone(t *testing.T) {
	want := map[string]string{`secret-key`: `prod/metatest`}
	c := RunnerDefinition{Kind: `metatest`, Name: `prod`, Variables: []Variable{
		{Name: `Token`, Type: typeSecret, Value: `shh`, Metadata: want},
	}}
	if err := c.RestoreMetadata("[metatest \"prod\"]\n\tToken=`shh`\n"); err != nil {
		t.Fatalf("RestoreMetadata failed: %v", err)
	}
	if !maps.Equal(c.Variables[0].Metadata, want) {
		t.Errorf("metadata was clobbered by a blob that said nothing: %#v", c.Variables[0].Metadata)
	}
	// while a blob that cannot be read leaves it alone too, and says so
	if err := c.RestoreMetadata("#gravwell-metadata \"Token\""); err == nil {
		t.Error("a malformed comment should fail RestoreMetadata")
	} else if !maps.Equal(c.Variables[0].Metadata, want) {
		t.Errorf("a failed restore changed the definition: %#v", c.Variables[0].Metadata)
	}
}

// TestEmptyMetadataWritesNothing keeps an absent map and an empty one rendering the same,
// so that adding and removing every note leaves the file it started as.
func TestEmptyMetadataWritesNothing(t *testing.T) {
	for _, md := range []map[string]string{nil, {}} {
		v := Variable{Name: `Token`, Type: typeSecret, Value: `shh`, Metadata: md}
		if c := v.metadataComment(); c != `` {
			t.Errorf("metadata %#v rendered %q", md, c)
		}
		var sb strings.Builder
		if err := v.emitIniMetadata(&sb, "\t"); err != nil {
			t.Fatalf("emitIniMetadata failed: %v", err)
		} else if sb.String() != `` {
			t.Errorf("metadata %#v emitted %q", md, sb.String())
		}
	}
}

// TestScanQuoted covers the scanner directly, principally the escaped quote and the
// escaped backslash before one: getting the second wrong reads the closing quote as part
// of the string and swallows the rest of the line.
func TestScanQuoted(t *testing.T) {
	for _, tc := range []struct {
		in   string
		val  string
		rest string
		bad  bool
	}{
		{in: `"a" rest`, val: `a`, rest: ` rest`},
		{in: `"a\"b"tail`, val: `a"b`, rest: `tail`},
		{in: `"a\\"tail`, val: `a\`, rest: `tail`},
		{in: `"a\\\"b"`, val: `a\"b`},
		{in: `""x`, rest: `x`},
		{in: `"\n\t\r\x00\xff"`, val: "\n\t\r\x00\xff"},
		{in: ``, bad: true},
		{in: `a"b"`, bad: true},
		{in: `"unterminated`, bad: true},
		{in: `"a\"`, bad: true},
		{in: `"\q"`, bad: true},
	} {
		val, rest, err := scanQuoted(tc.in)
		if tc.bad {
			if err == nil {
				t.Errorf("scanQuoted(%q) should have failed, got %q / %q", tc.in, val, rest)
			}
			continue
		}
		if err != nil {
			t.Errorf("scanQuoted(%q) failed: %v", tc.in, err)
		} else if val != tc.val || rest != tc.rest {
			t.Errorf("scanQuoted(%q) = %q / %q, wanted %q / %q", tc.in, val, rest, tc.val, tc.rest)
		}
	}
}

// TestMetadataDoesNotDisturbTheValues is the guard on the emission point: a comment is
// written above a member, never in place of one, and never inside a section it does not
// belong to.
func TestMetadataDoesNotDisturbTheValues(t *testing.T) {
	c := RunnerDefinition{Kind: `metatest`, Name: `prod`, Variables: []Variable{
		{Name: `Tag-Name`, Type: typeString, Value: `metatest`,
			Metadata: map[string]string{`source`: `default`}},
	}}
	ini, err := c.INI()
	if err != nil {
		t.Fatalf("INI failed: %v", err)
	}
	lines := strings.Split(strings.TrimRight(ini, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected a header, a comment and a value:\n%s", ini)
	}
	if !strings.HasPrefix(lines[0], `[metatest `) {
		t.Errorf("line 0 is %q", lines[0])
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[1]), iniMetadataComment) {
		t.Errorf("line 1 is %q", lines[1])
	}
	if strings.TrimSpace(lines[2]) != "Tag-Name=`metatest`" {
		t.Errorf("line 2 is %q", lines[2])
	}
}

// FuzzINIMetadataBlock is the standing guard on the two properties the whole scheme rests
// on: whatever a value holds, it cannot invent metadata that was never there, and
// whatever a key or value holds, the metadata that was there comes back exactly.
//
// Both are properties rather than cases because the interesting inputs are the ones
// nobody thought to write down: a value that spans lines, a key holding a comment
// character, a value that is itself a well formed metadata comment.
func FuzzINIMetadataBlock(f *testing.F) {
	f.Add(`plain`, `k`, `v`)
	f.Add("a\n\t#gravwell-metadata \"Forged\" \"a\"=\"b\"\nb", `k`, `v`)
	f.Add("a\n[other section]\n#gravwell-metadata X\nb", "\x00", "\xff")
	f.Add("spans\nlines\nraw", "\n", "\"")
	f.Add("a`b\\c\"d", " ", "\U0010ffff")
	f.Fuzz(func(t *testing.T, value, k, v string) {
		c := RunnerDefinition{Kind: `metatest`, Name: `prod`, Variables: []Variable{
			{Name: `Val`, Type: typeString, Value: value},
			{Name: `Tag`, Type: typeString, Value: `t`, Metadata: map[string]string{k: v}},
		}}
		ini, err := c.INI()
		if err != nil {
			// a value a config file cannot carry is refused, which is an answer
			if !errors.Is(err, ErrUnrepresentable) {
				t.Fatalf("INI failed with something other than ErrUnrepresentable: %v", err)
			}
			return
		}
		md, err := ParseINIMetadata(ini)
		if err != nil {
			t.Fatalf("ParseINIMetadata failed on:\n%s\nerr: %v", ini, err)
		}
		if len(md) != 1 {
			t.Fatalf("expected one variable's metadata, got %#v from:\n%s", md, ini)
		}
		if !maps.Equal(md[`Tag`], c.Variables[1].Metadata) {
			t.Fatalf("%q=%q came back as %#v from:\n%s", k, v, md[`Tag`], ini)
		}
	})
}

// TestIniWholeNumberAfterJSON pins down that a whole number survives the journey a value
// actually takes: set somewhere, stored as JSON, read back, rendered to a config file.
//
// The round trip is not optional in this design -- a server keeps definitions as JSON and
// the wire is JSON -- so every int and uint reaches emitIniLine as a float64.  fmt's %v
// renders a million as 1e+06, which the loader on the ingester then refuses, and the only
// symptom is the ingester reporting an error against a configuration that looks fine.
func TestIniWholeNumberAfterJSON(t *testing.T) {
	for _, tc := range []struct {
		name  string
		typ   ValueType
		value any
		want  string
	}{
		{`uint at a million`, typeUint, uint64(1000000), `Batch-Size=1000000`},
		{`int at a million`, typeInt, int64(1000000), `Batch-Size=1000000`},
		{`int beyond a billion`, typeInt, int64(123456789012), `Batch-Size=123456789012`},
		{`small uint`, typeUint, uint64(64), `Batch-Size=64`},
		{`zero`, typeInt, int64(0), `Batch-Size=0`},
		{`negative int`, typeInt, int64(-5), `Batch-Size=-5`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rd := RunnerDefinition{
				Kind: `okta`, Name: `one`, UUID: uuid.New(),
				Variables: []Variable{{Name: `Batch-Size`, Type: tc.typ, Value: tc.value}},
			}
			// the storage and wire round trip, which is what turns the value into a float64
			raw, err := json.Marshal(rd)
			if err != nil {
				t.Fatal(err)
			}
			var back RunnerDefinition
			if err = json.Unmarshal(raw, &back); err != nil {
				t.Fatal(err)
			}
			if _, ok := back.Variables[0].Value.(float64); !ok {
				t.Fatalf("expected the round trip to yield a float64, got %T",
					back.Variables[0].Value)
			}
			ini, err := back.INI()
			if err != nil {
				t.Fatalf("failed to render: %v", err)
			}
			if !strings.Contains(ini, tc.want) {
				t.Fatalf("expected %q in:\n%s", tc.want, ini)
			}
			if strings.Contains(ini, `e+`) {
				t.Fatalf("an exponent reached the config file:\n%s", ini)
			}
		})
	}
}
