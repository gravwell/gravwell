/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"time"

	"github.com/google/uuid"
)

// Secret is the metadata associated with a secret, it contains ownership information but not the associated value
type Secret struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Desc    string    `json:"description"`
	UID     int32     `json:"uid"`
	Groups  []int32   `json:"groups,omitempty"`
	Global  bool      `json:"global"`
	Created time.Time `json:"createdAt"`
}

// SecretCreate is the structure used to ask the API to make a new secret, only the request parameters are present
type SecretCreate struct {
	Name   string  `json:"name"`
	Desc   string  `json:"description"`
	Groups []int32 `json:"groups,omitempty"`
	Global bool    `json:"global"`
	Value  string  `json:"value"`
}

// SecretFull represents the full secret including its value.  This type is not sent through any traditional APIs
type SecretFull struct {
	Secret
	Value string `json:"value"`
}
