/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json/v2"

	"github.com/gravwell/gravwell/v4/utils/jsoncompat"
)

const (
	// request IDs
	FDG_REQ_GET_ENTRIES uint32 = 0x02000002
	FDG_REQ_STREAMING   uint32 = 0x02000005

	// response IDs
	FDG_RESP_GET_ENTRIES uint32 = 0x02000002
	FDG_RESP_STREAMING   uint32 = 0x02000005
)

type FdgRequest struct {
	BaseRequest
}

type FdgSet struct {
	Nodes  []Node
	Edges  []Edge
	Groups []string
}

type Node struct {
	Name  string
	Group int
}

type Edge struct {
	Value int64
	// Source and Destination nodes for an edge are represented by an index
	// into the parent node set
	Src int // index into the source node list
	Dst int // index into the destination node list
}

type FdgResponse struct {
	BaseResponse
	Entries FdgSet
}

func (x FdgResponse) MarshalJSON() ([]byte, error) {
	base, err := json.Marshal(x.BaseResponse, jsoncompat.Options)
	if err != nil {
		return nil, err
	}
	base[len(base)-1] = ','

	e, err := json.Marshal(&struct {
		Entries FdgSet
	}{
		Entries: x.Entries,
	}, jsoncompat.Options)
	if err != nil {
		return nil, err
	}

	return append(base, e[1:]...), nil
}
