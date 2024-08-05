/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffold

import (
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"golang.org/x/exp/constraints"
)

type Id_t interface {
	constraints.Integer | uuid.UUID
}

// Returns str converted to an id of type I.
// All hail the modern Library of Alexandira (https://stackoverflow.com/a/71048872).
func FromString[I Id_t](str string) (I, error) {
	var (
		err error
		ret I
	)

	switch p := any(&ret).(type) {
	case *uuid.UUID:
		var u uuid.UUID
		u, err = uuid.Parse(str)
		*p = u
	case *uint:
		var i uint64
		i, err = strconv.ParseUint(str, 10, 64)
		*p = uint(i)
	case *uint8:
		var i uint64
		i, err = strconv.ParseUint(str, 10, 8)
		*p = uint8(i)
	case *uint16:
		var i uint64
		i, err = strconv.ParseUint(str, 10, 16)
		*p = uint16(i)
	case *uint32:
		var i uint64
		i, err = strconv.ParseUint(str, 10, 32)
		*p = uint32(i)
	case *uint64:
		var i uint64
		i, err = strconv.ParseUint(str, 10, 64)
		*p = uint64(i)
	case *int:
		*p, err = strconv.Atoi(str)
	case *int8:
		var i int64
		i, err = strconv.ParseInt(str, 10, 8)
		*p = int8(i)
	case *int16:
		var i int64
		i, err = strconv.ParseInt(str, 10, 16)
		*p = int16(i)
	case *int32:
		var i int64
		i, err = strconv.ParseInt(str, 10, 32)
		*p = int32(i)
	case *int64:
		var i int64
		i, err = strconv.ParseInt(str, 10, 64)
		*p = int64(i)
	default:
		return ret, fmt.Errorf("unknown id type %#v", p)
	}
	return ret, err
}
