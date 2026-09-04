package types

import (
	"encoding/json/v2"
)

// An Optional type represents a field which may be unset during an update, preserving its prior value.
// If !Optional.IsSet(), this field will be omitted from a JSON marshal of the type.
type Optional[T any] struct {
	value T
	set   bool
}

// NewOptional returns a set field with value v.
func NewOptional[T any](v T) Optional[T] {
	return Optional[T]{value: v, set: true}
}

// Apply installs o's value into t iff o.IsSet().
func (o Optional[T]) Apply(t *T) {
	if o.IsSet() {
		*t = o.value
	}
}

// Set installs the given value and marks it as valid to include when marshaling.
func (o *Optional[T]) Set(v T) {
	o.value = v
	o.set = true
}

// Value fetches the value contained within the Optional.
// It is intended to prevent nil-derefs and is always safe to call.
// If !o.IsSet(), T's zero value will be returned
func (o Optional[T]) Value() T {
	if !o.IsSet() {
		var zero T
		return zero
	}
	return o.value
}

// IsSet states if this field has a value and thus will be included in the JSON representation.
func (o Optional[T]) IsSet() bool {
	return o.set
}

// Unset marks this field s.t. it will be skipped when marshaling.
func (o *Optional[T]) Unset() {
	o.set = false
}

// IsZero returns true iff !o.Set.
//
// This allows marshalers to omit unset values while still including zero values
// (assuming json.OmitZeroStructFields(true) or `json:",omitzero"` is set).
func (o Optional[T]) IsZero() bool {
	return !o.IsSet()
}

// MarshalJSON causes optional to always marshal to a safe value.
// If !o.IsSet(), T zero will be used.
func (o Optional[T]) MarshalJSON() ([]byte, error) {
	if !o.IsSet() {
		var zero T
		return json.Marshal(zero)
	}
	return json.Marshal(o.value)
}

// UnmarshalJSON decodes the given data into o's value and marks it as set.
func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &o.value); err != nil {
		return err
	}
	o.set = true
	return nil
}
