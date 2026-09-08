/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import "encoding/json"

type StackGraphValue struct {
	Label string
	Value int64
}

type StackGraphSet struct {
	Key    string
	Values []StackGraphValue
}

type StackGraphRequest struct {
	BaseRequest
}

type StackGraphResponse struct {
	BaseResponse
	Entries []StackGraphSet
}

func (sgs *StackGraphSet) Magnitude() (v int64) {
	if sgs == nil || len(sgs.Values) == 0 {
		return
	}
	for i := range sgs.Values {
		v += sgs.Values[i].Value
	}
	return
}

func (x StackGraphResponse) MarshalJSON() ([]byte, error) {
	base, err := json.Marshal(x.BaseResponse)
	if err != nil {
		return nil, err
	}
	base[len(base)-1] = ','

	e, err := json.Marshal(&struct {
		Entries []StackGraphSet
	}{
		Entries: x.Entries,
	})
	if err != nil {
		return nil, err
	}

	return append(base, e[1:]...), nil
}
