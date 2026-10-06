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
	"io"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"uuid"

	"github.com/gravwell/gravwell/v4/client/types"
)

const (
	// ingesterUUIDName is the config variable carrying the ingester UUID.  It is lifted out of
	// the variable list into Config.UUID so that INI does not emit it twice.
	ingesterUUIDName = `Ingester-UUID`

	// maxStructDepth guards against a config type that manages to reference itself.
	maxStructDepth = 8

	// dynamicTag is the struct tag this package reads for per member options:
	//
	//	Token string `dynamic:"secret"`
	//
	// It is separate from the gcfg and json tags because it describes how a member should
	// be presented and handled by the dynamic configuration system, not how it is parsed
	// or serialized.
	dynamicTag = `dynamic`

	// optSecret marks a member as a secret: a string whose value must never be shipped
	// and which a GUI should mask rather than display.
	optSecret = `secret`

	// optRequired marks a member that has to be populated for the configuration to be
	// usable.  It describes the configuration to whoever is filling it in, it is not a
	// substitute for the plugin's own Verify: a config can arrive from somewhere other
	// than a form.
	optRequired = `required`

	// optEnum lists the complete set of values a member may take:
	//
	//	Credentials_Type string `dynamic:"enum=static|environment|ec2role"`
	//
	// A member with one is drawn as a picker rather than a text box, which is the
	// difference between choosing a value and guessing one.  The separator is a pipe so
	// that a comma still separates one option from the next.
	optEnum = `enum`

	// optRequiredIf makes a member required only in the company of another:
	//
	//	AKID string `dynamic:"requiredif=Credentials-Type:|static"`
	//
	// A form is a flat list of fields and cannot say "needed only when", so without this
	// a conditionally required member is either marked required, which blocks the
	// configurations that genuinely do not need it, or marked optional, which lets
	// someone save one that cannot run.  An empty entry in the value list matches a
	// variable that is not set, which is how a member required alongside a default is
	// written: leaving the other field alone is still choosing its default.
	optRequiredIf = `requiredif`

	// iniRawUnsafe is the set of runes the gcfg raw string scanner treats specially:
	// a backtick, a backslash and a double quote.  A value holding none of them is
	// copied through a backtick string verbatim.
	//
	// A newline is deliberately not in the set.  A raw string may legally span lines, so
	// a value that carries one puts the rest of itself on lines of its own, and anything
	// splitting a rendered block on newlines would read those lines as structure.  The
	// answer to that is to read a block the way gcfg does rather than to narrow what a
	// value may hold: iniQuote can carry a newline but not a carriage return, so pushing
	// values onto it would turn every CRLF value from representable into refused.  See
	// scanINIComments, which tracks the strings instead.
	iniRawUnsafe = "`" + `\"`
)

var (
	ErrInvalidValueType = errors.New("invalid value type")
	ErrNotAStruct       = errors.New("config must be a struct")
	ErrUnsupportedType  = errors.New("unsupported config field type")
	ErrUnrepresentable  = errors.New("value cannot be represented in a config file")
	ErrAmbiguousMember  = errors.New("ambiguous promoted config member")

	uuidType = reflect.TypeFor[uuid.UUID]()
)

type ValueType string

const (
	typeBool        ValueType = `bool`
	typeInt         ValueType = `int`
	typeUint        ValueType = `uint`
	typeFloat       ValueType = `float`
	typeString      ValueType = `string`
	typeFile        ValueType = `file`   // identical to a string, but the GUI will treat this differently
	typeSecret      ValueType = `secret` // identical to a string, but we tell everyone we want to hide it
	typeUUID        ValueType = `uuid`
	typeSliceString ValueType = `[]string`
	typeStruct      ValueType = `struct`
	typeSliceStruct ValueType = `[]struct`
)

// RunnerDefinition represents a description of an ingester config or a populated and configured ingester.
// This struct is used to translate a native RunnerDefinition type from a plugin into something that can be shipped over JSON
// to a GUI/Webserver and drawn in a human friendly way.  It can then be sent to the ingester to be validated and
// translated back to an INI config blob.
type RunnerDefinition struct {
	Kind      string    // what configuration type this represents
	Name      string    // config name (key in config map for a given Kind)
	UUID      uuid.UUID `json:",omitzero"`
	Singleton bool      // whether more than one of this Kind can run at a time
	Variables []Variable
	Assigned  *Assignment     `json:",omitempty"`         // optional asisgnment for this config, will be empty for empty config prototypes
	Metadata  *RunnerMetadata `json:"metadata,omitempty"` // optional metadata for the runner which may contain an icon (svg) and/or documentation links
}

type DocLink struct {
	Name string `json:"name,omitempty"`
	Link string `json:"link,omitempty"`
}

type RunnerMetadata struct {
	Icon          string                 `json:"icon,omitempty"`          // optional SVG icon for the ingester
	Documentation []DocLink              `json:"documentation,omitempty"` // links to integration guides or ingester documentation
	Version       types.CanonicalVersion `json:"version,omitzero"`        // the plugin's own version, zero when it does not declare one
}

// MetadataProvider is an OPTIONAL interface a plugin config may implement to describe
// itself beyond its variables: an icon to draw it with, links to its documentation, and
// the version of the plugin that is offering it.
//
// It hangs off the config type rather than being passed to RegisterKind because the
// config type is the one thing every path already has in its hands, so a plugin that
// implements it is described everywhere without anything having to be threaded through.
// A plugin that does not implement it simply has no metadata, which is why every field of
// RunnerMetadata is optional.
//
// Implement it on the pointer receiver, the way plugin configs implement everything else.
// MapRunnerDefinition looks for it on both the value and a pointer to it.
type MetadataProvider interface {
	RunnerMetadata() *RunnerMetadata
}

// Version parses a canonical version string such as "1.2.3".
//
// A string that does not parse comes back as the zero value, which reports Enabled()
// false and is rendered as no version at all.  A plugin is not worth refusing to register
// over a typo in a field that exists to be printed next to its name, so the check for
// that belongs in a test rather than in a panic at init.
func Version(s string) (v types.CanonicalVersion) {
	v, _ = types.ParseCanonicalVersion(s)
	return
}

// metadataOf pulls the optional metadata off a plugin config.
//
// A config handed in by value cannot satisfy an interface whose methods are on the
// pointer receiver, and that is the normal way these are written, so a value gets copied
// somewhere addressable and asked again.  The copy is deliberate: nothing here should be
// able to hand a plugin a pointer into a caller's config.
func metadataOf(v any) *RunnerMetadata {
	if v == nil {
		return nil
	}
	if mp, ok := v.(MetadataProvider); ok {
		return mp.RunnerMetadata()
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		return nil // already a pointer, it simply does not implement it
	}
	pv := reflect.New(rv.Type())
	pv.Elem().Set(rv)
	if mp, ok := pv.Interface().(MetadataProvider); ok {
		return mp.RunnerMetadata()
	}
	return nil
}

// Condition names another variable and the values of it that make this one apply.
//
// An empty string among the values matches a variable that is not set, which is what a
// member that is only required alongside a default needs: the default is what an unset
// value means, so leaving it alone has to count as choosing it.
type Condition struct {
	Field  string
	Values []string `json:",omitempty"`
}

// Matches reports whether the condition holds given the value the named variable
// currently carries.  A nil value is treated as the empty string, see Condition.
func (c *Condition) Matches(v any) bool {
	if c == nil {
		return true
	}
	var cur string
	if v != nil {
		cur = fmt.Sprintf("%v", v)
	}
	return slices.Contains(c.Values, cur)
}

// Variable is a single value in a dynamic configuration, it may be some primative
// type like a string, int, float, etc.. or it may be a vector of primative types.
type Variable struct {
	Name        string
	Value       any `json:",omitempty"`
	Type        ValueType
	Description string `json:",omitempty"`
	Required    bool
	// various key/value metadata associated with this value, examples may be a secret that references a stored Secret by key
	Metadata map[string]string `json:",omitempty,omitzero"`

	// Enum, when set, is every value this variable may take.  A consumer should offer
	// exactly these and refuse anything else, and for a list it applies to each entry.
	Enum []string `json:",omitempty"`

	// RequiredWhen makes this variable required only while another one holds one of a set
	// of values.  Required and RequiredWhen are independent: Required means always.
	RequiredWhen *Condition `json:",omitempty"`
}

// RequiredNow reports whether a variable has to be filled in given what the rest of the
// configuration currently says.
//
// This is the question a form has to answer, and the variable cannot answer it alone: a
// member needed only alongside a particular choice elsewhere is required or not depending
// on what that choice currently is.
func (c RunnerDefinition) RequiredNow(v Variable) bool {
	if v.Required {
		return true
	}
	if v.RequiredWhen == nil {
		return false
	}
	for _, cur := range c.Variables {
		if cur.Name == v.RequiredWhen.Field {
			return v.RequiredWhen.Matches(cur.Value)
		}
	}
	// the variable it depends on is not in this configuration at all, so the condition
	// cannot hold and nothing is being asked for
	return false
}

// inEnum reports whether a value is one this variable allows.  A variable with no Enum
// allows everything.
func (v Variable) inEnum(s string) error {
	if len(v.Enum) == 0 {
		return nil
	}
	if slices.Contains(v.Enum, s) {
		return nil
	}
	return fmt.Errorf("%q is not a valid %s, it must be one of %s", s, v.Name, strings.Join(v.Enum, `, `))
}

// Assignment narrows which ingesters a configuration is meant for.  An empty Assignment,
// or a nil one, means the configuration goes to anything able to run its kind.
//
// The two lists are independent filters and both have to pass.  A configuration listing
// UUIDs goes only to those ingesters, one listing Classes goes only to ingesters in one
// of those classes, and one listing both goes only to an ingester that satisfies each.
// An empty list is not a filter, so listing classes alone does not restrict by UUID.
type Assignment struct {
	UUIDs   []uuid.UUID `json:",omitempty"` // specific ingester instances
	Classes []string    `json:",omitempty"` // classes of ingesters (hosted, http, etc...)
	Group   string      `json:",omitempty"` // a free form group (user configured)
}

// Empty reports whether this assignment narrows anything at all.
func (a *Assignment) Empty() bool {
	return a == nil || (len(a.UUIDs) == 0 && len(a.Classes) == 0 && a.Group == ``)
}

// AllowsUUID reports whether an ingester's UUID passes the UUID filter.  An assignment
// that lists no UUIDs does not filter on them.
func (a *Assignment) AllowsUUID(id uuid.UUID) bool {
	if a == nil || len(a.UUIDs) == 0 {
		return true
	}
	return slices.Contains(a.UUIDs, id)
}

// AllowsClass reports whether an ingester's class passes the class filter.  An assignment
// that lists no classes does not filter on them.
func (a *Assignment) AllowsClass(class string) bool {
	if a == nil || len(a.Classes) == 0 {
		return true
	}
	return slices.Contains(a.Classes, class)
}

// Validate checks a variable to make sure it is appropriately populated and that if there is a Value its type
// matches the ValueType
func (v Variable) Validate() error {
	if v.Name == `` {
		return errors.New("missing Name")
	} else if v.Type == `` {
		return errors.New("Missing Type")
	} else if err := v.Type.Valid(); err != nil {
		return err
	}
	if v.Value == nil {
		return nil // nothing to check, an empty prototype or an unset variable is fine
	}
	switch val := v.Value.(type) {
	case bool:
		return v.requireType(typeBool)
	case int, int8, int16, int32, int64:
		return v.requireType(typeInt)
	case uint, uint8, uint16, uint32, uint64:
		return v.requireType(typeUint)
	case float32:
		return v.requireType(typeFloat)
	case float64:
		// encoding/json hands back every number as a float64, so an integral
		// float64 is also an acceptable int or uint
		switch v.Type {
		case typeFloat:
		case typeInt:
			if val != math.Trunc(val) {
				return v.typeMismatch()
			}
		case typeUint:
			if val != math.Trunc(val) || val < 0 {
				return v.typeMismatch()
			}
		default:
			return v.typeMismatch()
		}
	case string:
		switch v.Type {
		case typeString:
			if err := v.inEnum(val); err != nil {
				return err
			}
		case typeFile:
		case typeSecret:
		case typeUUID:
			if _, err := uuid.Parse(val); err != nil {
				return fmt.Errorf("Value %q is not a valid uuid: %w", val, err)
			}
		default:
			return v.typeMismatch()
		}
	case uuid.UUID:
		return v.requireType(typeUUID)
	case []string:
		if err := v.requireType(typeSliceString); err != nil {
			return err
		}
		for _, cur := range val {
			if err := v.inEnum(cur); err != nil {
				return err
			}
		}
	case []any:
		// encoding/json hands back every array as a []any, so walk it to see what we really have
		switch v.Type {
		case typeSliceString:
			for i, x := range val {
				cur, ok := x.(string)
				if !ok {
					return fmt.Errorf("Value element %d is a %T, not a string", i, x)
				}
				if err := v.inEnum(cur); err != nil {
					return err
				}
			}
		case typeSliceStruct:
			for i, x := range val {
				if _, ok := x.(map[string]any); !ok {
					return fmt.Errorf("Value element %d is a %T, not a struct", i, x)
				}
			}
		default:
			return v.typeMismatch()
		}
	case map[string]any:
		return v.requireType(typeStruct)
	case []map[string]any:
		return v.requireType(typeSliceStruct)
	case []Variable:
		if v.Type != typeStruct {
			return v.typeMismatch()
		}
		for _, sub := range val {
			if err := sub.Validate(); err != nil {
				return fmt.Errorf("member %s is invalid: %w", sub.Name, err)
			}
		}
	case [][]Variable:
		if v.Type != typeSliceStruct {
			return v.typeMismatch()
		}
		for i, members := range val {
			for _, sub := range members {
				if err := sub.Validate(); err != nil {
					return fmt.Errorf("element %d member %s is invalid: %w", i, sub.Name, err)
				}
			}
		}
	default:
		return fmt.Errorf("Value type %T is not a supported dynamic config type", v.Value)
	}
	return nil
}

// requireType checks that the Variable is declared as the given ValueType.
func (v Variable) requireType(vt ValueType) error {
	if v.Type != vt {
		return v.typeMismatch()
	}
	return nil
}

// iniValueString renders s for the right hand side of an INI assignment.
//
// A raw backtick string is used when it is provably safe, purely because it is far easier
// to read in a config file.  The gcfg raw string scanner treats exactly three runes
// specially: a backtick terminates the string, a backslash sets its escape state, and a
// double quote gets escaped.  Every other rune is copied through verbatim, so a value
// holding none of those three cannot be altered by it no matter how that escaping logic
// changes.  Anything else takes the double quoted path, which is total over the values we
// support, so this is an optimization and never a correctness requirement.
func iniValueString(s string) (string, error) {
	if !strings.ContainsAny(s, iniRawUnsafe) {
		return "`" + s + "`", nil
	}
	return iniQuote(s)
}

// iniQuote renders s as a gcfg double quoted string.  This is the general path and can
// carry any value a config supports, including backticks and embedded newlines.
//
// The one exception is control characters.  The gcfg scanner understands exactly four
// escapes, \\ \" \n and \t, and has no numeric escape, so a control character other than
// a newline or a tab simply cannot be written into a config file.  Configs are not
// expected to carry them, so rather than mangle the value we reject it with
// ErrUnrepresentable and let the caller report a real error.
func iniQuote(s string) (string, error) {
	var sb strings.Builder
	sb.Grow(len(s) + 2)
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			sb.WriteString(`\\`)
		case '"':
			sb.WriteString(`\"`)
		case '\n':
			sb.WriteString(`\n`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				return ``, fmt.Errorf("%w: control character %q", ErrUnrepresentable, r)
			}
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String(), nil
}

// emitInitLine just encodes a variable into our INI line format
func (v Variable) emitIniLine(w io.Writer, prefix string) (err error) {
	// if the value is empty, do nothing
	if v.Value == nil {
		return
	}
	switch v.Type {
	case typeBool:
		fmt.Fprintf(w, "%s%s=%v\n", prefix, v.Name, v.Value)
	case typeInt, typeUint:
		// Every value in this system has been through JSON by the time it gets here: a
		// server stores definitions as JSON and the wire is JSON, so a whole number
		// arrives as a float64 no matter what set it.  %v on a float64 switches to
		// exponent form at a million, and Batch-Size=1e+06 is not something the config
		// loader on the far side will accept -- it fails at the ingester, which is the
		// one place that cannot explain why.
		fmt.Fprintf(w, "%s%s=%s\n", prefix, v.Name, iniWholeNumber(v.Value))
	case typeFloat:
		// %v uses the shortest representation that parses back exactly, %f would
		// silently truncate the value to six decimal places
		fmt.Fprintf(w, "%s%s=%v\n", prefix, v.Name, v.Value)
	case typeSecret:
		fallthrough // identical to a string
	case typeString:
		var q string
		if q, err = v.quote(fmt.Sprintf("%s", v.Value)); err != nil {
			return
		}
		fmt.Fprintf(w, "%s%s=%s\n", prefix, v.Name, q)
	case typeFile: // TODO FIXME - emit a filepath ?
	case typeUUID:
		// a uuid is only ever hex and dashes, it can never need escaping
		switch uv := v.Value.(type) {
		case string:
			fmt.Fprintf(w, "%s%s=%q\n", prefix, v.Name, uv)
		case uuid.UUID:
			fmt.Fprintf(w, "%s%s=%q\n", prefix, v.Name, uv.String())
		default:
			err = v.typeMismatch()
		}
	case typeSliceString:
		var set []string
		if set, err = v.stringSet(); err != nil {
			return
		}
		for _, x := range set {
			var q string
			if q, err = v.quote(x); err != nil {
				return
			}
			fmt.Fprintf(w, "%s%s=%s\n", prefix, v.Name, q)
		}
	// we may never support this, getting these into INI formats reliably without types
	// is going to be extremely difficult and may not be worth our time at all
	case typeStruct:
		// TODO FIXME
	case typeSliceStruct:
		// TODO FIXME
	}
	return
}

// iniWholeNumber renders an int or uint member without an exponent.
//
// A float64 is the shape a whole number takes after any JSON round trip, and fmt's %v
// would render a million as 1e+06.  A non-integral float cannot be a valid int or uint
// and Validate refuses it, so the fallback here is only for shapes that never reach a
// config file anyway.
func iniWholeNumber(v any) string {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case float32:
		f = float64(n)
	default:
		return fmt.Sprintf("%v", v)
	}
	if math.IsInf(f, 0) || math.IsNaN(f) || f != math.Trunc(f) {
		return fmt.Sprintf("%v", v)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// quote renders one of this variable's strings for an INI line, naming the variable on failure.
func (v Variable) quote(s string) (q string, err error) {
	if q, err = iniValueString(s); err != nil {
		err = fmt.Errorf("%s: %w", v.Name, err)
	}
	return
}

// stringSet pulls a typeSliceString value out as a []string.  A JSON round trip hands the
// slice back as a []any, so accept that shape too rather than silently emitting nothing.
func (v Variable) stringSet() (set []string, err error) {
	switch sv := v.Value.(type) {
	case []string:
		set = sv
	case []any:
		set = make([]string, 0, len(sv))
		for i, x := range sv {
			str, ok := x.(string)
			if !ok {
				err = fmt.Errorf("%s element %d is a %T, not a string", v.Name, i, x)
				return
			}
			set = append(set, str)
		}
	default:
		err = fmt.Errorf("%s: %w", v.Name, v.typeMismatch())
	}
	return
}

// typeMismatch reports that the concrete type of Value disagrees with the declared ValueType.
func (v Variable) typeMismatch() error {
	return fmt.Errorf("Value type %T does not match ValueType %s", v.Value, v.Type)
}

// ValidType checks that the given ValueType is one that we know how to handle.
// An unrecognized type results in an error wrapping ErrInvalidValueType.
func (vt ValueType) Valid() (err error) {
	switch vt {
	case typeBool:
	case typeInt:
	case typeUint:
	case typeFloat:
	case typeString:
	case typeFile:
	case typeSecret:
	case typeUUID:
	case typeSliceString:
	case typeStruct:
	case typeSliceStruct:
	default:
		err = fmt.Errorf("%w %q", ErrInvalidValueType, string(vt))
	}
	return
}

// Complex indicates if a ValueType is not a primitive type.
// non-primative types include slices and structs
func (vt ValueType) Complex() bool {
	switch vt {
	case typeBool:
	case typeInt:
	case typeUint:
	case typeFloat:
	case typeString:
	case typeSecret:
	case typeUUID:
	case typeSliceString:
	default:
		return true
	}
	return false
}

// MapRunnerDefinition takes a native plugin config struct and maps it to the dynamic RunnerDefinition structure.
// Embedded structs are flattened, because gcfg promotes them into the parent INI section.
// Unexported members are skipped, they are derived at verify time and are not config.
// A member tagged dynamic:"secret" is described as a secret rather than a string, and a
// member tagged either dynamic:"secret" or json:"-" has its value withheld: the variable
// is described so a GUI can ask for it, but the current value is never populated.
// A member tagged dynamic:"required" is marked Required so a form can insist on it.  The
// options combine, dynamic:"secret,required" is a credential that has to be supplied.  Zero valued members are described without a value, so handing this a zero
// struct yields an empty prototype and handing it a populated struct yields a populated config.
func MapRunnerDefinition(kind, name string, v any) (c RunnerDefinition, err error) {
	if kind == `` {
		err = errors.New("empty kind")
		return
	} else if name == `` {
		err = errors.New("empty name")
		return
	} else if v == nil {
		err = fmt.Errorf("%w, got nil", ErrNotAStruct)
		return
	}
	rv := derefValue(reflect.ValueOf(v))
	if rv.Kind() != reflect.Struct {
		err = fmt.Errorf("%w, got %T", ErrNotAStruct, v)
		return
	}
	var vars []Variable
	if vars, err = mapStruct(rv, 0); err != nil {
		return
	}
	c = RunnerDefinition{
		Kind:      kind,
		Name:      name,
		Variables: make([]Variable, 0, len(vars)),
		// a plugin that describes itself is described here, so every path that maps a
		// config carries the icon, the docs and the version without asking for them
		Metadata: metadataOf(v),
	}
	for _, vr := range vars {
		if err = vr.Validate(); err != nil {
			err = fmt.Errorf("%s produced an invalid variable: %w", vr.Name, err)
			return
		}
		if vr.Name == ingesterUUIDName {
			// INI writes this from RunnerDefinition.UUID, keep it out of the variable set
			if s, ok := vr.Value.(string); ok {
				if c.UUID, err = uuid.Parse(s); err != nil {
					err = fmt.Errorf("Invalid Ingester-UUID %q %w", s, err)
					return
				}
			}
			continue
		}
		c.Variables = append(c.Variables, vr)
	}
	return
}

// mappedVar is a Variable plus how far it was promoted to reach the struct being
// flattened.  A member declared directly on that struct is depth 0, one pulled out of an
// embedded struct is depth 1, and so on.
type mappedVar struct {
	Variable
	depth int
}

// mapStruct walks the exported members of a struct and produces a Variable for each,
// flattening any embedded structs into the same list because gcfg promotes them into the
// parent INI section rather than giving them a subsection.
func mapStruct(rv reflect.Value, depth int) (vars []Variable, err error) {
	var mapped []mappedVar
	if mapped, err = mapStructMembers(rv, depth); err != nil {
		return
	}
	return resolveShadowed(mapped)
}

// mapStructMembers does the actual walk, tracking promotion depth so that the caller can
// apply the shadowing rules.
func mapStructMembers(rv reflect.Value, depth int) (vars []mappedVar, err error) {
	if depth > maxStructDepth {
		err = fmt.Errorf("config is nested deeper than %d structs", maxStructDepth)
		return
	}
	rt := rv.Type()
	for i := range rt.NumField() {
		f := rt.Field(i)
		// An embedded struct promotes its exported members even when the embedded type
		// itself is unexported, and gcfg populates them happily, so recurse into it
		// before the export check that skips an ordinary unexported member.  This is the
		// same rule encoding/json applies when it walks a struct.
		if f.Anonymous && derefType(f.Type).Kind() == reflect.Struct {
			var sub []mappedVar
			if sub, err = mapStructMembers(derefValue(rv.Field(i)), depth+1); err != nil {
				return
			}
			for _, sv := range sub {
				sv.depth++ // one promotion further out than where it was declared
				vars = append(vars, sv)
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		var nv Variable
		if nv, err = mapField(f, rv.Field(i), depth); err != nil {
			return
		}
		vars = append(vars, mappedVar{Variable: nv})
	}
	return
}

// resolveShadowed applies Go's own field promotion rules to a flattened member list: a
// member declared closer to the outside shadows a promoted one of the same name.  That
// matters because gcfg resolves an INI key the same way, so emitting both would write a
// key that only ever lands on the outer member and silently leave the inner one unset.
// Two members promoted from the same depth are genuinely ambiguous, neither Go nor gcfg
// can address them, so that is reported rather than guessed at.
func resolveShadowed(mapped []mappedVar) (vars []Variable, err error) {
	shallowest := make(map[string]int, len(mapped))
	ambiguous := make(map[string]bool, len(mapped))
	for _, m := range mapped {
		switch d, ok := shallowest[m.Name]; {
		case !ok || m.depth < d:
			shallowest[m.Name] = m.depth
			ambiguous[m.Name] = false
		case m.depth == d:
			ambiguous[m.Name] = true
		}
	}
	vars = make([]Variable, 0, len(mapped))
	seen := make(map[string]bool, len(mapped))
	for _, m := range mapped {
		if m.depth != shallowest[m.Name] || seen[m.Name] {
			continue // shadowed by a member closer to the outside, or already emitted
		}
		if ambiguous[m.Name] {
			err = fmt.Errorf("%w %q", ErrAmbiguousMember, m.Name)
			return
		}
		seen[m.Name] = true
		vars = append(vars, m.Variable)
	}
	return
}

// mapField produces a single Variable from a struct member.
func mapField(f reflect.StructField, fv reflect.Value, depth int) (v Variable, err error) {
	v.Name = iniName(f)
	ft := derefType(f.Type)
	if v.Type, err = valueTypeOf(ft); err != nil {
		err = fmt.Errorf("%s: %w", v.Name, err)
		return
	}
	// a member tagged dynamic:"required" has to be filled in, which lets a form say so
	// up front rather than letting someone save a configuration the ingester will refuse
	v.Required = hasDynamicOption(f, optRequired)

	// a member tagged dynamic:"enum=a|b|c" may only hold one of those, so a form can
	// offer them rather than leaving someone to find the set by trial and error
	if raw, ok := dynamicOption(f, optEnum); ok {
		if v.Enum = splitTagList(raw, false); len(v.Enum) == 0 {
			err = fmt.Errorf("%s: %w, an enum must list at least one value", v.Name, ErrUnsupportedType)
			return
		}
		// an enum is a set of strings, on the member itself or on each of its entries
		if v.Type != typeString && v.Type != typeSliceString {
			err = fmt.Errorf("%s: %w, an enum must be a string or a []string, got %s",
				v.Name, ErrUnsupportedType, ft)
			return
		}
		// and never on a secret.  A secret is drawn masked and its value is withheld, so
		// there is nothing to offer a choice of, and the two tags together would be
		// accepted and then quietly do nothing: the control stays a password box and the
		// set is never enforced, because a withheld value is not one anything checks.
		if hasDynamicOption(f, optSecret) {
			err = fmt.Errorf("%s: %w, a secret cannot carry an enum, its value is never shown or offered",
				v.Name, ErrUnsupportedType)
			return
		}
	}

	// a member tagged dynamic:"requiredif=Other:a|b" is required only while the variable
	// it names holds one of those values
	if raw, ok := dynamicOption(f, optRequiredIf); ok {
		field, values, found := strings.Cut(raw, `:`)
		if field = strings.TrimSpace(field); !found || field == `` {
			err = fmt.Errorf("%s: %w, requiredif must be written Field:value", v.Name, ErrUnsupportedType)
			return
		}
		v.RequiredWhen = &Condition{Field: field, Values: splitTagList(values, true)}
		if len(v.RequiredWhen.Values) == 0 {
			err = fmt.Errorf("%s: %w, requiredif must name at least one value", v.Name, ErrUnsupportedType)
			return
		}
	}

	// a member tagged dynamic:"secret" is a string that a GUI should mask.  It is typed
	// as a secret rather than a string so that every consumer knows, rather than each one
	// having to guess from the member's name.
	secret := hasDynamicOption(f, optSecret)
	if secret {
		if v.Type != typeString {
			err = fmt.Errorf("%s: %w, a secret must be a string, got %s", v.Name, ErrUnsupportedType, ft)
			return
		}
		v.Type = typeSecret
	}
	// a secret's value never leaves the ingester, and neither does a json:"-" member's.
	// Both are described so a GUI can ask for them, but the current value is withheld:
	// shipping a live credential to a browser to render is how they escape.
	if secret || f.Tag.Get(`json`) == `-` {
		return
	}
	if fv = derefValue(fv); !fv.IsValid() || fv.IsZero() {
		return // unset, leave Value nil
	}
	if v.Value, err = fieldValue(v.Type, fv, depth); err != nil {
		err = fmt.Errorf("%s: %w", v.Name, err)
	}
	return
}

// hasDynamicOption reports whether a member carries an option in its dynamic tag.
//
// The tag value is a comma separated list so that further options can be added without
// invalidating the ones already written into plugin configs.  Note the spelling: Go
// struct tags are `dynamic:"secret"`, a space after the colon stops the conventional tag
// parser from finding the key at all.
func hasDynamicOption(f reflect.StructField, opt string) bool {
	for cur := range strings.SplitSeq(f.Tag.Get(dynamicTag), `,`) {
		if strings.TrimSpace(cur) == opt {
			return true
		}
	}
	return false
}

// dynamicOption returns the value of a name=value option in the dynamic tag.
//
// The tag stays comma separated, so a value that is itself a list uses a pipe: a comma
// inside one would end the option rather than extend it.
func dynamicOption(f reflect.StructField, name string) (string, bool) {
	for cur := range strings.SplitSeq(f.Tag.Get(dynamicTag), `,`) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(cur), name+`=`); ok {
			return strings.TrimSpace(v), true
		}
	}
	return ``, false
}

// splitTagList splits a pipe separated tag value.  Empty entries are kept or dropped by
// the caller: they are meaningless in an enum and meaningful in a condition, where one
// stands for "not set".
func splitTagList(raw string, keepEmpty bool) (r []string) {
	for cur := range strings.SplitSeq(raw, `|`) {
		cur = strings.TrimSpace(cur)
		if cur == `` && !keepEmpty {
			continue
		}
		r = append(r, cur)
	}
	return
}

// iniName returns the name gcfg would match this member against, honoring a gcfg ident
// override and otherwise swapping the underscores in the Go name for dashes.
func iniName(f reflect.StructField) string {
	if ident, _, _ := strings.Cut(f.Tag.Get(`gcfg`), `,`); ident != `` {
		return ident
	}
	return strings.ReplaceAll(f.Name, `_`, `-`)
}

// valueTypeOf maps a Go type onto the ValueType used to describe it.
func valueTypeOf(t reflect.Type) (vt ValueType, err error) {
	if t == uuidType {
		return typeUUID, nil
	}
	switch t.Kind() {
	case reflect.Bool:
		vt = typeBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		vt = typeInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		vt = typeUint
	case reflect.Float32, reflect.Float64:
		vt = typeFloat
	case reflect.String:
		vt = typeString // named string types (mimecast Api, msgraph ContentType) land here too
	case reflect.Struct:
		vt = typeStruct
	case reflect.Slice:
		et := derefType(t.Elem())
		switch {
		case et == uuidType:
			err = fmt.Errorf("%w %s", ErrUnsupportedType, t)
		case et.Kind() == reflect.String:
			vt = typeSliceString
		case et.Kind() == reflect.Struct:
			vt = typeSliceStruct
		default:
			err = fmt.Errorf("%w %s", ErrUnsupportedType, t)
		}
	default:
		err = fmt.Errorf("%w %s", ErrUnsupportedType, t)
	}
	return
}

// fieldValue extracts a member's value in the representation Variable.Validate expects.
func fieldValue(vt ValueType, fv reflect.Value, depth int) (val any, err error) {
	switch vt {
	case typeBool:
		val = fv.Bool()
	case typeInt:
		val = fv.Int()
	case typeUint:
		val = fv.Uint()
	case typeFloat:
		val = fv.Float()
	case typeString, typeSecret:
		val = fv.String()
	case typeUUID:
		val = fv.Interface().(uuid.UUID).String()
	case typeSliceString:
		set := make([]string, fv.Len())
		for i := range set {
			set[i] = derefValue(fv.Index(i)).String()
		}
		val = set
	case typeStruct:
		var sub []Variable
		if sub, err = mapStruct(fv, depth+1); err != nil {
			return
		}
		val = sub
	case typeSliceStruct:
		set := make([][]Variable, fv.Len())
		for i := range set {
			if set[i], err = mapStruct(derefValue(fv.Index(i)), depth+1); err != nil {
				return
			}
		}
		val = set
	default:
		err = fmt.Errorf("%w %s", ErrInvalidValueType, vt)
	}
	return
}

// derefType follows pointers to the type actually being pointed at.
func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// derefValue follows pointers to the value actually being pointed at.  A nil pointer yields
// a zero value of the pointed at type so that we can still describe the variable.
func derefValue(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.New(v.Type().Elem()).Elem()
		}
		v = v.Elem()
	}
	return v
}

// INI writes out a
func (c RunnerDefinition) INI() (r string, err error) {
	// check the required stuff
	if c.Kind == `` {
		err = errors.New("empty kind")
		return
	} else if c.Name == `` {
		err = errors.New("empty name")
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "[%s %q]\n", c.Kind, c.Name)
	if c.UUID != uuid.Nil() {
		fmt.Fprintf(&sb, "\tIngester-UUID=%s\n", c.UUID)
	}
	// the flat members first, then the nested ones, because gcfg reads keys as belonging
	// to the section they follow: a subsection opened partway down would swallow every
	// key written after it
	var todo []Variable
	for _, v := range c.Variables {
		// the metadata comment goes above the member it describes, and it goes there
		// whether or not that member has a value to write below it
		if err = v.emitIniMetadata(&sb, "\t"); err != nil {
			return
		}
		if v.Type.Complex() {
			// only one that was actually filled in has anything to write.  A nested
			// member the operator left alone is described but unset, exactly like every
			// other unset member, and refusing the whole configuration over it would make
			// any plugin that merely declares an optional one impossible to configure.
			if v.Value != nil {
				todo = append(todo, v)
			}
			continue
		} else if err = v.emitIniLine(&sb, "\t"); err != nil {
			return
		}
	}
	// A nested member that was filled in cannot be written yet, and saying so is the whole
	// point: this used to collect them and then return the block without them, so a
	// configuration went to disk missing entire sections and nothing anywhere reported a
	// problem.  A value that cannot be represented is refused, which is what every other
	// unrepresentable value here already does.
	if len(todo) > 0 {
		names := make([]string, 0, len(todo))
		for _, v := range todo {
			names = append(names, v.Name)
		}
		err = fmt.Errorf("%w: %s cannot be written to a config file yet, nested members are unsupported",
			ErrUnrepresentable, strings.Join(names, `, `))
		return
	}
	r = sb.String()
	return
}
