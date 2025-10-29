//go:build linux
// +build linux

/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package caps

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	linuxCapV3 = 0x20080522
)

type capHeader struct {
	version uint32
	pid     int
}

type capData struct {
	effective   uint32
	permitted   uint32
	inheritable uint32
}

type Capabilities uint64

func GetCaps() (c Capabilities, err error) {
	//check if we are running as root, if so, just return ALL caps
	if os.Getuid() == 0 || os.Geteuid() == 0 {
		c = All
		return
	}
	hdr := capHeader{
		version: linuxCapV3,
	}
	var data [2]capData
	_, _, e1 := unix.RawSyscall(unix.SYS_CAPGET, uintptr(unsafe.Pointer(&hdr)), uintptr(unsafe.Pointer(&data)), 0)
	if e1 != 0 {
		err = e1
		return
	}
	c = Capabilities(uint64(data[0].effective) | (uint64(data[1].effective) << 32))
	return
}

func (c Capabilities) Has(v Capabilities) bool {
	return (c & (1 << v)) != 0
}

func Has(v Capabilities) bool {
	if c, err := GetCaps(); err == nil {
		return c.Has(v)
	}
	return false
}
