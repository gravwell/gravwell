/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json"
)

type TextRequest struct {
	BaseRequest
}

type TextResponse struct {
	BaseResponse
	Entries []SearchEntry   `json:",omitempty"`
	Explore []ExploreResult `json:",omitempty"`
}

type RawResponse struct {
	BaseResponse
	Entries []SearchEntry   `json:",omitempty"`
	Explore []ExploreResult `json:",omitempty"`
}

type RawRequest struct {
	BaseRequest
}

func (tr TextResponse) MarshalJSON() ([]byte, error) {
	type alias TextResponse
	return json.Marshal(&struct {
		alias
		Entries emptyEntries
	}{
		alias:   alias(tr),
		Entries: emptyEntries(tr.Entries),
	})
}
