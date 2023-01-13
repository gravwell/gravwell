/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

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
	Nodes  []Node   `json:"nodes"`
	Edges  []Edge   `json:"links"`
	Groups []string `json:"groups"`
}

type Node struct {
	Name  string `json:"name"`
	Group int    `json:"group"`
}

type Edge struct {
	Value int64 `json:"value"`
	// Source and Destination nodes for an edge are represented by an index
	// into the parent node set
	Src int `json:"source"` // index into the source node list
	Dst int `json:"target"` // index into the destination node list
}

type FdgResponse struct {
	BaseResponse
	Entries FdgSet
}
