package types

import (
	"bytes"
	"encoding/gob"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
)

// A Nullable represents a field that is always present in a JSON
// object but whose value may be null. It's the counterpart to Optional[T].
// Optional is for the request bodies where an absent field means "leave this
// unchanged", Nullable is for response fields that are required but nullable
// per the API spec (ex: AssetCommonFields.DeletedAt). The key is always
// emitted, and null represents "no value" rather than the field being missing.
type Nullable[T any] struct {
	value T
	valid bool
}

// NewNullable returns a non-null Nullable holding v.
func NewNullable[T any](v T) Nullable[T] {
	return Nullable[T]{value: v, valid: true}
}

// Set installs v and marks n as non-null.
func (n *Nullable[T]) Set(v T) {
	n.value = v
	n.valid = true
}

// SetNull marks n as null, discarding any previously set value.
func (n *Nullable[T]) SetNull() {
	var zero T
	n.value = zero
	n.valid = false
}

// Value fetches the value contained within the Nullable.
// It is intended to prevent nil-derefs and is always safe to call.
// If n.IsNull(), T's zero value is returned.
func (n Nullable[T]) Value() T {
	return n.value
}

// IsNull reports whether n currently represents JSON null.
func (n Nullable[T]) IsNull() bool {
	return !n.valid
}

// IsZero always reports false. Nullable represents a field that must
// always be present in its enclosing JSON object. null stands in for
// "no value" instead of omitting the key. So it must never be skipped
// by a `,omitzero` tag or OmitZeroStructFields. If you want omit-when-empty
// semantics, use Optional[T] instead.
func (n Nullable[T]) IsZero() bool {
	return false
}

// IsOpaqueValue reports that Nullable[T] is an opaque, self-marshaling
// value: its own fields are private implementation detail, not a composite
// record. Reflection-based tooling that walks a struct's fields (e.g. this
// module's utils/weave package) should treat a Nullable[T] field as a
// single leaf rather than recursing into it.
func (n Nullable[T]) IsOpaqueValue() bool {
	return true
}

// MarshalJSONTo writes JSON null when n.IsNull(), and the marshaled value otherwise.
func (n Nullable[T]) MarshalJSONTo(enc *jsontext.Encoder) error {
	if !n.valid {
		return enc.WriteToken(jsontext.Null)
	}

	return json.MarshalEncode(enc, n.value)
}

// UnmarshalJSONFrom decodes a JSON null into a null Nullable, or any other
// value into a non-null Nullable holding the decoded value.
func (n *Nullable[T]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if dec.PeekKind() == jsontext.KindNull {
		if _, err := dec.ReadToken(); err != nil {
			return err
		}

		var zero T
		n.value, n.valid = zero, false

		return nil
	}

	if err := json.UnmarshalDecode(dec, &n.value); err != nil {
		return err
	}

	n.valid = true

	return nil
}

// String implements fmt.Stringer so Nullable[T] renders sensibly wherever
// it's stringified via %v/fmt.Sprintf. Without this, the zero value's private
// fields print as a raw struct dump instead of something readable.
func (n Nullable[T]) String() string {
	if n.IsNull() {
		return "null"
	}
	return fmt.Sprint(n.value)
}

// gobNullable mirrors Nullable[T]'s private fields with exported names so
// encoding/gob (which requires at least one exported field, and has no
// notion of the JSON hooks above) has something to encode.
type gobNullable[T any] struct {
	Value T
	Valid bool
}

// GobEncode implements gob.GobEncoder.
func (n Nullable[T]) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(gobNullable[T]{Value: n.value, Valid: n.valid}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GobDecode implements gob.GobDecoder.
func (n *Nullable[T]) GobDecode(data []byte) error {
	var g gobNullable[T]
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&g); err != nil {
		return err
	}
	n.value, n.valid = g.Value, g.Valid
	return nil
}
