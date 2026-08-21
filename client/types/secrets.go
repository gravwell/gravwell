/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import "encoding/json"

// Secret is the metadata associated with a secret. It contains
// ownership information but not the associated value. This is the
// primary type that will be returned by standard user APIs; the SecretFull
// type is only returned via a special searchagent-specific endpoint.
type Secret struct {
	CommonFields
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (s Secret) MarshalJSON() ([]byte, error) {
	type dummySecret Secret
	s.CommonFields = s.CommonFields.MakeNilSlices()
	return json.Marshal(dummySecret(s))
}

// SecretCreate is the structure used to ask the API to make a new secret.
type SecretCreate struct {
	CommonFields
	Value string
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (s SecretCreate) MarshalJSON() ([]byte, error) {
	type dummySecretCreate SecretCreate
	s.CommonFields = s.CommonFields.MakeNilSlices()
	return json.Marshal(dummySecretCreate(s))
}

// SecretFull is returned only to the searchagent through a special endpoint.
type SecretFull struct {
	CommonFields
	Value string
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (s SecretFull) MarshalJSON() ([]byte, error) {
	type dummySecretFull SecretFull
	s.CommonFields = s.CommonFields.MakeNilSlices()
	return json.Marshal(dummySecretFull(s))
}

// SecretListResponse is returned when listing secrets.
type SecretListResponse struct {
	BaseListResponse
	Results []Secret
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (s SecretListResponse) MarshalJSON() ([]byte, error) {
	type dummySecretListResponse SecretListResponse
	s.Results = nonNilSlice(s.Results)
	s.BaseListResponse = s.BaseListResponse.MakeNilSlices()
	return json.Marshal(dummySecretListResponse(s))
}
