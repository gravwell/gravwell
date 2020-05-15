/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"strconv"
	"strings"
)

// ParseSource attempts to parse a string as a source override
// The priority logic is:
//     1. IP address
//     2. Numeric ID
//     3. hexadecimal hash
func ParseSource(v string) (ret net.IP, err error) {
	var r uint64
	var x []byte
	if v = strings.TrimSpace(v); len(v) == 0 {
		err = errors.New("Empty override")
		return
	}
	if ret = net.ParseIP(v); len(ret) == 4 || len(ret) == 16 {
		return
	} else if r, err = ParseInt(v); err == nil {
		ret = make(net.IP, 16)
		binary.BigEndian.PutUint64(ret[8:], r)
		return
	}
	//the string length must be > 0 && <= 32 && even
	if len(v) == 0 || len(v) > 32 || (len(v)&0x1) != 0 {
		err = errors.New("invalid source override")
	}
	if x, err = hex.DecodeString(v); err == nil {
		if len(x) > 16 {
			err = errors.New("source override too large")
			return
		}
		if len(x) <= 4 {
			ret = make([]byte, 4)
		} else {
			ret = make([]byte, 16)
		}
		offset := len(ret) - len(x)
		copy(ret[offset:], x)
	}
	return
}

func ParseInt(v string) (r uint64, err error) {
	if strings.HasPrefix(v, `0x`) {
		r, err = strconv.ParseUint(strings.TrimPrefix(v, `0x`), 16, 64)
	} else {
		r, err = strconv.ParseUint(v, 10, 64)
	}
	return
}
