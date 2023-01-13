//go:build gofuzz
// +build gofuzz

/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"time"
)

var (
	mpa = newMultipartAssembler(1024*1024, time.Second)
)

func FuzzCiscoISEParser(data []byte) int {
	var msg iseMessage
	if err := msg.Parse(string(data), nil, true); err == nil {
		return 1
	}
	return 0
}

func FuzzCiscoISEAssembler(data []byte) int {
	var rih remoteISE
	if err := rih.Parse(string(data)); err == nil {
		if _, _, bad := mpa.add(rih, nil); bad {
			return 1
		}
		return 1
	}
	return 0
}
