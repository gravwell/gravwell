/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
)

type PatchType interface {
	MacroPatch | FilePatch | TokenPatch | AXPatch | SavedQueryPatch | ResourcePatch | TemplatePatch |
		UserPreferencePatch | SecretPatch | SecretValuePatch | ScheduledSearchPatch | ScheduledScriptPatch |
		FlowPatch | AlertPatch | PlaybookPatch | DashboardPatch | ActionablePatch | UserPatch | GroupPatch
}

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

// MarshalJSONTo causes optional to always marshal to a safe value.
// If !o.IsSet(), T zero will be used.
//
// NOTE(rlandau): implemented as MarshalJSONTo instead of MarshalJSON in order to propagate encoder
// option (likely jsoncompat.Opts).
func (o Optional[T]) MarshalJSONTo(enc *jsontext.Encoder) error {
	if !o.IsSet() {
		var zero T
		return json.MarshalEncode(enc, zero)
	}
	return json.MarshalEncode(enc, o.value)
}

// UnmarshalJSONFrom decodes the given data into o's value and marks it as set.
//
// NOTE(rlandau): implemented as MarshalJSONFrom instead of MarshalJSON in order to propagate encoder
// option (likely jsoncompat.Opts).
func (o *Optional[T]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if err := json.UnmarshalDecode(dec, &o.value); err != nil {
		return err
	}
	o.set = true
	return nil
}
