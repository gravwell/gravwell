/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datagram

import "reflect"

// Calculate the byte size of s without padding. Only works with flat structs
func packetSizeOf(s any) uintptr {
	t := reflect.TypeOf(s)

	var size uintptr
	for i := 0; i < t.NumField(); i++ {
		size += t.Field(i).Type.Size()
	}

	return size
}
