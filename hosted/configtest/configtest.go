/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package configtest provides test helpers for plugin config types that implement
// the hosted.Config interface.
//
// Equal implementations are hand written field by field, which means a field added
// to a config later can quietly go unchecked and the runner will then refuse to
// restart an ingester whose config really did change. CheckEqual walks the config
// with reflection and fails if any exported field is ignored by Equal.
package configtest

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/gravwell/gravwell/v4/hosted"
)

// CheckEqual validates that *T implements hosted.Config correctly.
// It checks the basic contract (a config equals itself in both value and pointer
// form, and never equals nil or a foreign type) and then mutates each exported
// field in turn, requiring Equal to report a difference for every one of them.
//
// Unexported fields cannot be set through reflection and are not covered, so
// derived state such as parsed override maps still needs a hand written test.
func CheckEqual[T any](t *testing.T, base T) {
	t.Helper()
	cfg, ok := any(&base).(hosted.Config)
	if !ok {
		t.Fatalf("%T does not implement hosted.Config", &base)
	}

	// the basic contract, this also catches an Equal that just always returns false
	self := base
	if !cfg.Equal(&self) {
		t.Fatal("Equal returned false for an identical config pointer")
	}
	if !cfg.Equal(self) {
		t.Fatal("Equal returned false for an identical config value")
	}
	if cfg.Equal(nil) {
		t.Error("Equal returned true for a nil any")
	}
	if cfg.Equal((*T)(nil)) {
		t.Error("Equal returned true for a nil config pointer")
	}
	if cfg.Equal(struct{ Nonsense int }{}) {
		t.Error("Equal returned true for a foreign type")
	}

	typ := reflect.TypeOf(base)
	if typ.Kind() != reflect.Struct {
		t.Fatalf("config type %s is not a struct", typ)
	}

	for _, f := range leaves(t, typ, nil, "") {
		mutated := base
		v := reflect.ValueOf(&mutated).Elem().FieldByIndex(f.index)
		if err := mutate(v); err != nil {
			t.Fatalf("cannot mutate field %s: %v", f.name, err)
		}
		if cfg.Equal(&mutated) {
			t.Errorf("Equal ignores field %s, it must be added to the comparison", f.name)
		}
	}
}

type leaf struct {
	index []int
	name  string
}

// leaves collects every exported non-struct field, descending through nested and
// embedded structs so that each individual value gets its own mutation.
func leaves(t *testing.T, typ reflect.Type, prefix []int, path string) (r []leaf) {
	t.Helper()
	var exported int
	for i := range typ.NumField() {
		sf := typ.Field(i)
		if sf.PkgPath != `` {
			continue // unexported, reflection cannot set it
		}
		exported++
		index := append(append([]int{}, prefix...), i)
		name := sf.Name
		if path != `` {
			name = path + "." + name
		}
		if sf.Type.Kind() == reflect.Struct {
			r = append(r, leaves(t, sf.Type, index, name)...)
			continue
		}
		r = append(r, leaf{index: index, name: name})
	}
	if exported == 0 {
		t.Fatalf("struct %s at %q has no exported fields, CheckEqual cannot cover it", typ, path)
	}
	return
}

// mutate sets v to a value that differs from the one it currently holds.
func mutate(v reflect.Value) error {
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(!v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(v.Int() + 1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(v.Uint() + 1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(v.Float() + 1)
	case reflect.String:
		v.SetString(v.String() + "-configtest")
	case reflect.Slice:
		// build a fresh backing array so we never scribble on the original
		nv := reflect.MakeSlice(v.Type(), v.Len(), v.Len()+1)
		reflect.Copy(nv, v)
		v.Set(reflect.Append(nv, reflect.New(v.Type().Elem()).Elem()))
	case reflect.Map:
		nv := reflect.MakeMapWithSize(v.Type(), v.Len()+1)
		iter := v.MapRange()
		for iter.Next() {
			nv.SetMapIndex(iter.Key(), iter.Value())
		}
		key := reflect.New(v.Type().Key()).Elem()
		for nv.MapIndex(key).IsValid() { // walk until we land on a key that is not in use
			if err := mutate(key); err != nil {
				return err
			}
		}
		nv.SetMapIndex(key, reflect.New(v.Type().Elem()).Elem())
		v.Set(nv)
	default:
		return fmt.Errorf("unsupported kind %s, extend configtest.mutate", v.Kind())
	}
	return nil
}
