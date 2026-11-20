/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"bytes"
	"encoding/gob"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
)

type nullablePatchState uint8

const (
	npOmit nullablePatchState = iota
	npValue
	npNull
)

// NullablePatchIn is an inbound-only type that represents a field in a PATCH request that is:
// a value, when that value should replace the current value;
// null, when the current value should be dropped/clobbered;
// or omitted, when the current value should be retained.
//
// The default state is to be omitted.
//
// It use only be used in special cases carved out in the api spec (.spectral/null-exceptions.json).
//
// Every NullablePatchIn field MUST be tagged `json:",omitzero"` and MUST NOT be tagged omitempty.
// Trying to marshal an omitted field without omitzero is an error
// (it would indistinguishably marshal to the zero value otherwise).
type NullablePatchIn[T any] struct {
	value T
	state nullablePatchState
}

// NewNullablePatchIn returns a NullablePatchIn that will marshal to v.
func NewNullablePatchIn[T any](v T) NullablePatchIn[T] {
	return NullablePatchIn[T]{value: v, state: npValue}
}

// Apply installs n's state into dst:
// a value procs dst.Set(), null procs dst.SetNull, and omitted leaves dst untouched.
func (n NullablePatchIn[T]) Apply(dst *Nullable[T]) {
	switch n.state {
	case npValue:
		dst.Set(n.value)
	case npNull:
		dst.SetNull()
	}
}

// Set installs the given value and makes n marshal to t
// (rather than omitting or marshaling to `null`).
func (n *NullablePatchIn[T]) Set(t T) {
	n.value = t
	n.state = npValue
}

// Null makes n marshal to `null`, discarding any previously set value.
func (n *NullablePatchIn[T]) Null() {
	var zero T
	n.value = zero
	n.state = npNull
}

// Omit makes n omitted when marshaling, discarding any previously set value.
func (n *NullablePatchIn[T]) Omit() {
	var zero T
	n.value = zero
	n.state = npOmit
}

// Value fetches the value contained within the NullablePatchIn.
// If set to null or omit, the zero value is returned.
func (n NullablePatchIn[T]) Value() T {
	if n.state == npValue {
		return n.value
	}
	var zero T
	return zero
}

// IsNull returns if n would marshal to `null`.
func (n NullablePatchIn[T]) IsNull() bool {
	return n.state == npNull
}

// IsOmitted returns if n would be skipped while marshaling.
func (n NullablePatchIn[T]) IsOmitted() bool {
	return n.state == npOmit
}

// ErrOmittedNullablePatchIn is returned when marshaling an omitted NullablePatchIn that was not
// skipped by omitzero.
// See the NullablePatchIn doc for why this is an error.
var ErrOmittedNullablePatchIn = errors.New(
	"an omitted NullablePatchIn has no JSON form; tag the field `json:\",omitzero\"`")

// MarshalJSONTo marshals n according to its internal state:
// a value marshals to that value,
// null marshals to `null`,
// and omitted won't even call MarshalJSONTo
// (unless you forgot to add the omitzero tag, in which case this is an error).
func (n NullablePatchIn[T]) MarshalJSONTo(enc *jsontext.Encoder) error {
	switch n.state {
	case npNull:
		return enc.WriteToken(jsontext.Null)
	case npOmit:
		return ErrOmittedNullablePatchIn
	case npValue:
		return json.MarshalEncode(enc, n.value)
	default:
		return fmt.Errorf("unknown NullablePatchIn state: %v", n.state)
	}
}

// IsZero returns true iff n is set to omitted.
// Zero values will still be marshaled if n is .Set().
func (n NullablePatchIn[T]) IsZero() bool {
	return n.IsOmitted()
}

// IsOpaqueValue reports that NullablePatchIn[T] is an opaque, self-marshaling value.
// See Optional[T].IsOpaqueValue for more information.
func (n NullablePatchIn[T]) IsOpaqueValue() bool {
	return true
}

// UnmarshalJSONFrom decodes the given data into n's value.
// `null` marks n as null; anything else is decoded into a fresh T.
func (n *NullablePatchIn[T]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	// This won't even get called if the field is omitted,
	// which is perfect because the default state is "omitted".
	if dec.PeekKind() == jsontext.KindNull {
		// an UnmarshalerFrom must consume exactly one value, so swallow the null
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		n.Null()
		return nil
	}
	// decode into a fresh T so maps/structs do not merge with a prior value
	var v T
	if err := json.UnmarshalDecode(dec, &v); err != nil {
		return err
	}
	n.Set(v)
	return nil
}

// String implements fmt.Stringer for the same reason Nullable[T] does (see Nullable[T].String).
// A null NullablePatchIn[T] prints "null"; an omitted one prints T's zero value.
func (n NullablePatchIn[T]) String() string {
	if n.IsNull() {
		return "null"
	}
	return fmt.Sprint(n.Value())
}

// Equal reports whether n and k represent the same value:
// both omitted, both null, or both set with deeply-equal underlying values.
// See Nullable[T].Equal for why this exists.
func (n NullablePatchIn[T]) Equal(k NullablePatchIn[T]) bool {
	if n.state != k.state {
		return false
	}
	if n.state != npValue {
		return true
	}
	return reflect.DeepEqual(n.value, k.value)
}

// gobNullablePatchIn mirrors NullablePatchIn[T]'s private fields with exported names.
// See gobNullable for why this exists.
type gobNullablePatchIn[T any] struct {
	Value T
	State nullablePatchState
}

// GobEncode implements gob.GobEncoder.
func (n NullablePatchIn[T]) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(gobNullablePatchIn[T]{Value: n.value, State: n.state}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GobDecode implements gob.GobDecoder.
func (n *NullablePatchIn[T]) GobDecode(data []byte) error {
	var g gobNullablePatchIn[T]
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&g); err != nil {
		return err
	}
	if g.State > npNull {
		return fmt.Errorf("unknown NullablePatchIn state: %v", g.State)
	}
	n.value, n.state = g.Value, g.State
	return nil
}
