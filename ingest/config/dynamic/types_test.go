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
	"uuid"

	"github.com/gravwell/gcfg"
)

const testUUID = `3a1f5c22-0c4e-4d9a-9b1e-5f2d7c8a0011`

// emitTarget is what a single emitted variable gets parsed back into.  Every member is a
// slice so that a repeated key is captured rather than rejected.
type emitTarget struct {
	Sect map[string]*struct {
		Str  []string
		Num  []int64
		UNum []uint64
		Flt  []float64
		Flag []bool
		ID   []string
	}
}

// emit renders a single variable and returns the generated INI line.
func emit(t *testing.T, v Variable) string {
	t.Helper()
	var sb strings.Builder
	if err := v.emitIniLine(&sb, "\t"); err != nil {
		t.Fatalf("emitIniLine(%s/%s) unexpected error: %v", v.Name, v.Type, err)
	}
	return sb.String()
}

// parseBack feeds an emitted line through the real gcfg parser so that the tests assert
// against what a config file actually yields rather than against our own formatting.
func parseBack(t *testing.T, line string) *struct {
	Str  []string
	Num  []int64
	UNum []uint64
	Flt  []float64
	Flag []bool
	ID   []string
} {
	t.Helper()
	var tgt emitTarget
	src := "[sect \"x\"]\n" + line
	if err := gcfg.ReadStringInto(&tgt, src); err != nil {
		t.Fatalf("gcfg rejected %q: %v", src, err)
	}
	return tgt.Sect["x"]
}

func TestValueTypeValid(t *testing.T) {
	all := []ValueType{typeBool, typeInt, typeUint, typeFloat, typeString,
		typeUUID, typeSliceString, typeStruct, typeSliceStruct}
	for _, vt := range all {
		if err := vt.Valid(); err != nil {
			t.Errorf("%s should be valid: %v", vt, err)
		}
	}
	for _, vt := range []ValueType{``, `int64`, `String`, `[]int`, `bogus`, `[]bool`} {
		err := vt.Valid()
		if err == nil {
			t.Errorf("%q should be invalid", vt)
		} else if !errors.Is(err, ErrInvalidValueType) {
			t.Errorf("%q: error does not wrap ErrInvalidValueType: %v", vt, err)
		}
	}
}

func TestValueTypeComplex(t *testing.T) {
	for _, vt := range []ValueType{typeBool, typeInt, typeUint, typeFloat, typeString, typeUUID, typeSliceString} {
		if vt.Complex() {
			t.Errorf("%s is primitive but Complex() says otherwise", vt)
		}
	}
	for _, vt := range []ValueType{typeStruct, typeSliceStruct} {
		if !vt.Complex() {
			t.Errorf("%s is complex but Complex() says otherwise", vt)
		}
	}
}

func TestValidateRequiredFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    Variable
	}{
		{`no name`, Variable{Type: typeString}},
		{`no type`, Variable{Name: `X`}},
		{`bad type`, Variable{Name: `X`, Type: `bogus`}},
	} {
		if err := tc.v.Validate(); err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
	// a variable with no value at all is a legitimate prototype
	if err := (Variable{Name: `X`, Type: typeString}).Validate(); err != nil {
		t.Errorf("prototype variable should validate: %v", err)
	}
}

// TestTypeBool covers the bool primitive end to end.
func TestTypeBool(t *testing.T) {
	if vt, err := valueTypeOf(reflectTypeOf(true)); err != nil || vt != typeBool {
		t.Fatalf("valueTypeOf(bool) = %v, %v", vt, err)
	}
	// accepted
	for _, val := range []any{true, false} {
		if err := (Variable{Name: `Flag`, Type: typeBool, Value: val}).Validate(); err != nil {
			t.Errorf("bool %v should validate: %v", val, err)
		}
	}
	// rejected against every other type
	for _, vt := range []ValueType{typeInt, typeString, typeFloat, typeUUID, typeSliceString} {
		if err := (Variable{Name: `Flag`, Type: vt, Value: true}).Validate(); err == nil {
			t.Errorf("bool value should not validate as %s", vt)
		}
	}
	for _, tc := range []struct {
		val  bool
		want bool
	}{{true, true}, {false, false}} {
		got := parseBack(t, emit(t, Variable{Name: `Flag`, Type: typeBool, Value: tc.val}))
		if len(got.Flag) != 1 || got.Flag[0] != tc.want {
			t.Errorf("bool %v round tripped to %v", tc.val, got.Flag)
		}
	}
}

// TestTypeInt covers every signed width plus the float64 shape a JSON round trip produces.
func TestTypeInt(t *testing.T) {
	for _, v := range []any{int(1), int8(1), int16(1), int32(1), int64(1)} {
		if vt, err := valueTypeOf(reflectTypeOf(v)); err != nil || vt != typeInt {
			t.Errorf("valueTypeOf(%T) = %v, %v", v, vt, err)
		}
		if err := (Variable{Name: `Num`, Type: typeInt, Value: v}).Validate(); err != nil {
			t.Errorf("%T should validate as int: %v", v, err)
		}
	}
	// encoding/json hands numbers back as float64, an integral one is a legal int
	if err := (Variable{Name: `Num`, Type: typeInt, Value: float64(24)}).Validate(); err != nil {
		t.Errorf("integral float64 should validate as int: %v", err)
	}
	if err := (Variable{Name: `Num`, Type: typeInt, Value: float64(24.5)}).Validate(); err == nil {
		t.Error("fractional float64 should not validate as int")
	}
	for _, bad := range []any{`24`, true, []string{`24`}} {
		if err := (Variable{Name: `Num`, Type: typeInt, Value: bad}).Validate(); err == nil {
			t.Errorf("%T should not validate as int", bad)
		}
	}
	for _, val := range []any{int64(0), int64(42), int64(-17), int64(1 << 40), float64(24)} {
		got := parseBack(t, emit(t, Variable{Name: `Num`, Type: typeInt, Value: val}))
		if len(got.Num) != 1 {
			t.Fatalf("int %v produced %d values", val, len(got.Num))
		}
		var want int64
		switch x := val.(type) {
		case int64:
			want = x
		case float64:
			want = int64(x)
		}
		if got.Num[0] != want {
			t.Errorf("int %v round tripped to %v", val, got.Num[0])
		}
	}
}

// TestTypeUint covers every unsigned width and the sign check on the JSON float64 shape.
func TestTypeUint(t *testing.T) {
	for _, v := range []any{uint(1), uint8(1), uint16(1), uint32(1), uint64(1)} {
		if vt, err := valueTypeOf(reflectTypeOf(v)); err != nil || vt != typeUint {
			t.Errorf("valueTypeOf(%T) = %v, %v", v, vt, err)
		}
		if err := (Variable{Name: `UNum`, Type: typeUint, Value: v}).Validate(); err != nil {
			t.Errorf("%T should validate as uint: %v", v, err)
		}
	}
	if err := (Variable{Name: `UNum`, Type: typeUint, Value: float64(7)}).Validate(); err != nil {
		t.Errorf("integral float64 should validate as uint: %v", err)
	}
	for _, bad := range []any{float64(-1), float64(1.5), int64(1)} {
		if err := (Variable{Name: `UNum`, Type: typeUint, Value: bad}).Validate(); err == nil {
			t.Errorf("%v (%T) should not validate as uint", bad, bad)
		}
	}
	for _, val := range []uint64{0, 42, 1 << 40} {
		got := parseBack(t, emit(t, Variable{Name: `UNum`, Type: typeUint, Value: val}))
		if len(got.UNum) != 1 || got.UNum[0] != val {
			t.Errorf("uint %v round tripped to %v", val, got.UNum)
		}
	}
}

// TestTypeFloat covers the float primitive.
func TestTypeFloat(t *testing.T) {
	for _, v := range []any{float32(1), float64(1)} {
		if vt, err := valueTypeOf(reflectTypeOf(v)); err != nil || vt != typeFloat {
			t.Errorf("valueTypeOf(%T) = %v, %v", v, vt, err)
		}
		if err := (Variable{Name: `Flt`, Type: typeFloat, Value: v}).Validate(); err != nil {
			t.Errorf("%T should validate as float: %v", v, err)
		}
	}
	for _, bad := range []any{int64(1), `1.5`, true} {
		if err := (Variable{Name: `Flt`, Type: typeFloat, Value: bad}).Validate(); err == nil {
			t.Errorf("%T should not validate as float", bad)
		}
	}
	for _, val := range []float64{0, 1.5, -2.25, 3.14159265358979, 1e12} {
		got := parseBack(t, emit(t, Variable{Name: `Flt`, Type: typeFloat, Value: val}))
		if len(got.Flt) != 1 {
			t.Fatalf("float %v produced %d values", val, len(got.Flt))
		}
		if got.Flt[0] != val {
			t.Errorf("float %v round tripped to %v (line %q)", val, got.Flt[0],
				strings.TrimSpace(emit(t, Variable{Name: `Flt`, Type: typeFloat, Value: val})))
		}
	}
}

// TestTypeString covers the string primitive and the quoting rules.
func TestTypeString(t *testing.T) {
	if vt, err := valueTypeOf(reflectTypeOf(``)); err != nil || vt != typeString {
		t.Fatalf("valueTypeOf(string) = %v, %v", vt, err)
	}
	// a named string type, as mimecast Api and msgraph ContentType are declared
	type namedString string
	if vt, err := valueTypeOf(reflectTypeOf(namedString(``))); err != nil || vt != typeString {
		t.Fatalf("valueTypeOf(named string) = %v, %v", vt, err)
	}
	if err := (Variable{Name: `Str`, Type: typeString, Value: `x`}).Validate(); err != nil {
		t.Errorf("string should validate: %v", err)
	}
	for _, bad := range []any{int64(1), true, []string{`x`}} {
		if err := (Variable{Name: `Str`, Type: typeString, Value: bad}).Validate(); err == nil {
			t.Errorf("%T should not validate as string", bad)
		}
	}

	for _, tc := range []struct {
		name     string
		in       string
		backtick bool // whether we expect the readable raw form
	}{
		{`plain`, `hello world`, true},
		{`empty`, ``, true},
		{`semicolon`, `a;b`, true},
		{`hash`, `a#b`, true},
		{`equals`, `k=v`, true},
		{`brackets`, `[not a section]`, true},
		{`unicode`, `héllo→世界`, true},
		{`env`, `${HOME}`, true},
		{`trailing space`, `pad  `, true},
		{`newline`, "line1\nline2", true},
		// a carriage return can only go on the raw path at all: gcfg has no escape for it
		{`carriage return`, "line1\rline2", true},
		{`crlf`, "line1\r\nline2", true},
		{`tab`, "a\tb", true},
		{`double quote`, `say "hi"`, false},
		{`backslash`, `C:\path\to`, false},
		{`backslash quote`, `a\"b`, false},
		{`backtick`, "has`tick", false},
		{`backtick and quote`, "a`\"b", false},
	} {
		line := emit(t, Variable{Name: `Str`, Type: typeString, Value: tc.in})
		if raw := strings.HasPrefix(line, "\tStr=`"); raw != tc.backtick {
			t.Errorf("%s: backtick form = %v, want %v (line %q)", tc.name, raw, tc.backtick, line)
		}
		if got := parseBack(t, line).Str; len(got) != 1 || got[0] != tc.in {
			t.Errorf("%s: %q round tripped to %q via %q", tc.name, tc.in, got, line)
		}
	}
}

// TestStringControlChars documents that a control character cannot be carried in a config.
func TestStringControlChars(t *testing.T) {
	// backticks pass raw bytes straight through, so a lone control char survives
	if got := parseBack(t, emit(t, Variable{Name: `Str`, Type: typeString, Value: "a\x01b"})).Str; got[0] != "a\x01b" {
		t.Errorf("control char via backticks round tripped to %q", got)
	}
	// but once the value forces the quoted path there is no escape for it
	err := (Variable{Name: `Str`, Type: typeString, Value: "a`b\x01c"}).emitIniLine(&strings.Builder{}, ``)
	if err == nil {
		t.Fatal("expected an error for a control char on the quoted path")
	} else if !errors.Is(err, ErrUnrepresentable) {
		t.Errorf("error does not wrap ErrUnrepresentable: %v", err)
	}
}

// TestTypeUUID covers the uuid primitive in both its string and native forms.
func TestTypeUUID(t *testing.T) {
	if vt, err := valueTypeOf(reflectTypeOf(uuid.UUID{})); err != nil || vt != typeUUID {
		t.Fatalf("valueTypeOf(uuid.UUID) = %v, %v", vt, err)
	}
	parsed, err := uuid.Parse(testUUID)
	if err != nil {
		t.Fatal(err)
	}
	for _, val := range []any{testUUID, parsed} {
		if err := (Variable{Name: `ID`, Type: typeUUID, Value: val}).Validate(); err != nil {
			t.Errorf("%T should validate as uuid: %v", val, err)
		}
		if got := parseBack(t, emit(t, Variable{Name: `ID`, Type: typeUUID, Value: val})).ID; len(got) != 1 || got[0] != testUUID {
			t.Errorf("%T round tripped to %q", val, got)
		}
	}
	// a string that is not a uuid must be caught
	if err := (Variable{Name: `ID`, Type: typeUUID, Value: `not-a-uuid`}).Validate(); err == nil {
		t.Error("a malformed uuid string should not validate")
	}
	for _, bad := range []any{int64(1), true} {
		if err := (Variable{Name: `ID`, Type: typeUUID, Value: bad}).Validate(); err == nil {
			t.Errorf("%T should not validate as uuid", bad)
		}
	}
}

// TestTypeSliceString covers the one vector primitive, in both the native and JSON shapes.
func TestTypeSliceString(t *testing.T) {
	if vt, err := valueTypeOf(reflectTypeOf([]string{})); err != nil || vt != typeSliceString {
		t.Fatalf("valueTypeOf([]string) = %v, %v", vt, err)
	}
	type namedString string
	if vt, err := valueTypeOf(reflectTypeOf([]namedString{})); err != nil || vt != typeSliceString {
		t.Fatalf("valueTypeOf([]named string) = %v, %v", vt, err)
	}

	want := []string{`plain`, `say "hi"`, "has`tick", `C:\path`}
	// the native []string and the []any that a JSON round trip yields must behave identically
	for _, val := range []any{want, []any{`plain`, `say "hi"`, "has`tick", `C:\path`}} {
		if err := (Variable{Name: `Str`, Type: typeSliceString, Value: val}).Validate(); err != nil {
			t.Errorf("%T should validate: %v", val, err)
		}
		got := parseBack(t, emit(t, Variable{Name: `Str`, Type: typeSliceString, Value: val})).Str
		if len(got) != len(want) {
			t.Fatalf("%T produced %d values, want %d: %q", val, len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%T element %d = %q, want %q", val, i, got[i], want[i])
			}
		}
	}
	// an empty slice emits nothing at all
	if line := emit(t, Variable{Name: `Str`, Type: typeSliceString, Value: []string{}}); line != `` {
		t.Errorf("empty slice emitted %q", line)
	}
	// a slice holding something other than strings is an error, not a silent drop
	if err := (Variable{Name: `Str`, Type: typeSliceString, Value: []any{1, 2}}).emitIniLine(&strings.Builder{}, ``); err == nil {
		t.Error("expected an error for a []any of non strings")
	}
	if err := (Variable{Name: `Str`, Type: typeSliceString, Value: []any{1}}).Validate(); err == nil {
		t.Error("[]any of non strings should not validate")
	}
}

// TestEmitPrototype checks that a variable with no value contributes no INI line, which is
// what keeps prototypes and withheld secrets from producing malformed output.
func TestEmitPrototype(t *testing.T) {
	for _, vt := range []ValueType{typeBool, typeInt, typeUint, typeFloat, typeString, typeUUID, typeSliceString} {
		if line := emit(t, Variable{Name: `X`, Type: vt}); line != `` {
			t.Errorf("%s with no value emitted %q", vt, line)
		}
	}
}

// TestUnsupportedTypes checks the member types we deliberately refuse.
func TestUnsupportedTypes(t *testing.T) {
	for _, v := range []any{map[string]string{}, []int{}, []bool{}, make(chan int), [4]byte{}} {
		if vt, err := valueTypeOf(reflectTypeOf(v)); err == nil {
			t.Errorf("%T should be unsupported, got %s", v, vt)
		} else if !errors.Is(err, ErrUnsupportedType) {
			t.Errorf("%T: error does not wrap ErrUnsupportedType: %v", v, err)
		}
	}
}

// reflectTypeOf keeps the type table above readable.
func reflectTypeOf(v any) reflect.Type { return reflect.TypeOf(v) }
