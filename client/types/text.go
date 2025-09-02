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
	ContainsBinaryEntries bool            //just a flag to tell the GUI that we might have data that needs some help
	Entries               []SearchEntry   `json:",omitempty"`
	Explore               []ExploreResult `json:",omitempty"`
	printableData         bool            // true if the search entries have printable DATA fields.
}

func (r *RawResponse) SetPrintableData(b bool) {
	r.printableData = b
}

type RawRequest struct {
	BaseRequest
}

func (tr TextResponse) MarshalJSON() ([]byte, error) {
	base, err := json.Marshal(tr.BaseResponse)
	if err != nil {
		return nil, err
	}
	base[len(base)-1] = ','

	e, err := json.Marshal(&struct {
		Entries emptyEntries
		Explore []ExploreResult `json:",omitempty"`
	}{
		Entries: emptyEntries(tr.Entries),
		Explore: tr.Explore,
	})
	if err != nil {
		return nil, err
	}

	return append(base, e[1:]...), nil
}
