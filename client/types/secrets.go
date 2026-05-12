/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

// Secret is the metadata associated with a secret. It contains
// ownership information but not the associated value. This is the
// primary type that will be returned by standard user APIs; the SecretFull
// type is only returned via a special searchagent-specific endpoint.
type Secret struct {
	CommonFields
}

// SecretCreate is the structure used to ask the API to make a new secret.
type SecretCreate struct {
	CommonFields
	Value string
}

// SecretFull is returned only to the searchagent through a special endpoint.
type SecretFull struct {
	CommonFields
	Value string
}

// SecretListResponse is returned when listing secrets.
type SecretListResponse struct {
	BaseListResponse
	Results []Secret `json:"results"`
}
