// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

// Code generated by 'go generate'; DO NOT EDIT.

package wineventlog

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var _ unsafe.Pointer

// Do the interface allocations only once for common
// Errno values.
const (
	errnoERROR_IO_PENDING = 997
)

var (
	errERROR_IO_PENDING error = syscall.Errno(errnoERROR_IO_PENDING)
)

// errnoErr returns common boxed Errno values, to prevent
// allocations at runtime.
func errnoErr(e syscall.Errno) error {
	switch e {
	case 0:
		return nil
	case errnoERROR_IO_PENDING:
		return errERROR_IO_PENDING
	}
	// TODO: add more here, after collecting data on the common
	// error values see on Windows. (perhaps when running
	// all.bat?)
	return e
}

var (
	modwevtapi = windows.NewLazySystemDLL("wevtapi.dll")
	modole32   = windows.NewLazySystemDLL("ole32.dll")

	procEvtOpenLog                  = modwevtapi.NewProc("EvtOpenLog")
	procEvtQuery                    = modwevtapi.NewProc("EvtQuery")
	procEvtSubscribe                = modwevtapi.NewProc("EvtSubscribe")
	procEvtCreateBookmark           = modwevtapi.NewProc("EvtCreateBookmark")
	procEvtUpdateBookmark           = modwevtapi.NewProc("EvtUpdateBookmark")
	procEvtCreateRenderContext      = modwevtapi.NewProc("EvtCreateRenderContext")
	procEvtRender                   = modwevtapi.NewProc("EvtRender")
	procEvtClose                    = modwevtapi.NewProc("EvtClose")
	procEvtSeek                     = modwevtapi.NewProc("EvtSeek")
	procEvtNext                     = modwevtapi.NewProc("EvtNext")
	procEvtOpenChannelEnum          = modwevtapi.NewProc("EvtOpenChannelEnum")
	procEvtNextChannelPath          = modwevtapi.NewProc("EvtNextChannelPath")
	procEvtFormatMessage            = modwevtapi.NewProc("EvtFormatMessage")
	procEvtOpenPublisherMetadata    = modwevtapi.NewProc("EvtOpenPublisherMetadata")
	procEvtGetLogInfo               = modwevtapi.NewProc("EvtGetLogInfo")
	procEvtOpenChannelConfig        = modwevtapi.NewProc("EvtOpenChannelConfig")
	procEvtGetChannelConfigProperty = modwevtapi.NewProc("EvtGetChannelConfigProperty")
	procStringFromGUID2             = modole32.NewProc("StringFromGUID2")
)

func _EvtOpenLog(session EvtHandle, path *uint16, flags uint32) (handle EvtHandle, err error) {
	r0, _, e1 := syscall.Syscall(procEvtOpenLog.Addr(), 3, uintptr(session), uintptr(unsafe.Pointer(path)), uintptr(flags))
	handle = EvtHandle(r0)
	if handle == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtGetLogInfo(session EvtHandle, id EvtLogPropertyId, buffsize uint32, buff *byte, used *uint32) (err error) {
	r0, _, e1 := syscall.Syscall6(procEvtGetLogInfo.Addr(), 5, uintptr(session), uintptr(id), uintptr(buffsize), uintptr(unsafe.Pointer(buff)), uintptr(unsafe.Pointer(used)), 0)
	if r0 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtOpenChannelConfig(session EvtHandle, path *uint16) (handle EvtHandle, err error) {
	r0, _, e1 := syscall.Syscall(procEvtOpenChannelConfig.Addr(), 3, uintptr(session), uintptr(unsafe.Pointer(path)), uintptr(0))
	handle = EvtHandle(r0)
	if handle == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtGetChannelConfigProperty(handle EvtHandle, id EvtChannelConfigPropertyId, buffsize uint32, buff *byte, used *uint32) (err error) {
	r0, _, e1 := syscall.Syscall6(procEvtGetChannelConfigProperty.Addr(), 6, uintptr(handle), uintptr(id), 0, uintptr(buffsize), uintptr(unsafe.Pointer(buff)), uintptr(unsafe.Pointer(used)))
	if r0 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtQuery(session EvtHandle, path *uint16, query *uint16, flags uint32) (handle EvtHandle, err error) {
	r0, _, e1 := syscall.Syscall6(procEvtQuery.Addr(), 4, uintptr(session), uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(query)), uintptr(flags), 0, 0)
	handle = EvtHandle(r0)
	if handle == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtSubscribe(session EvtHandle, signalEvent uintptr, channelPath *uint16, query *uint16, bookmark EvtHandle, context uintptr, callback syscall.Handle, flags EvtSubscribeFlag) (handle EvtHandle, err error) {
	r0, _, e1 := syscall.Syscall9(procEvtSubscribe.Addr(), 8, uintptr(session), uintptr(signalEvent), uintptr(unsafe.Pointer(channelPath)), uintptr(unsafe.Pointer(query)), uintptr(bookmark), uintptr(context), uintptr(callback), uintptr(flags), 0)
	handle = EvtHandle(r0)
	if handle == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtCreateBookmark(bookmarkXML *uint16) (handle EvtHandle, err error) {
	r0, _, e1 := syscall.Syscall(procEvtCreateBookmark.Addr(), 1, uintptr(unsafe.Pointer(bookmarkXML)), 0, 0)
	handle = EvtHandle(r0)
	if handle == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtUpdateBookmark(bookmark EvtHandle, event EvtHandle) (err error) {
	r1, _, e1 := syscall.Syscall(procEvtUpdateBookmark.Addr(), 2, uintptr(bookmark), uintptr(event), 0)
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtCreateRenderContext(ValuePathsCount uint32, valuePaths uintptr, flags EvtRenderContextFlag) (handle EvtHandle, err error) {
	r0, _, e1 := syscall.Syscall(procEvtCreateRenderContext.Addr(), 3, uintptr(ValuePathsCount), uintptr(valuePaths), uintptr(flags))
	handle = EvtHandle(r0)
	if handle == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtRender(context EvtHandle, fragment EvtHandle, flags EvtRenderFlag, bufferSize uint32, buffer *byte, bufferUsed *uint32, propertyCount *uint32) (err error) {
	r1, _, e1 := syscall.Syscall9(procEvtRender.Addr(), 7, uintptr(context), uintptr(fragment), uintptr(flags), uintptr(bufferSize), uintptr(unsafe.Pointer(buffer)), uintptr(unsafe.Pointer(bufferUsed)), uintptr(unsafe.Pointer(propertyCount)), 0, 0)
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtClose(object EvtHandle) (err error) {
	r1, _, e1 := syscall.Syscall(procEvtClose.Addr(), 1, uintptr(object), 0, 0)
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtSeek(resultSet EvtHandle, position int64, bookmark EvtHandle, timeout uint32, flags uint32) (success bool, err error) {
	r0, _, e1 := syscall.Syscall6(procEvtSeek.Addr(), 5, uintptr(resultSet), uintptr(position), uintptr(bookmark), uintptr(timeout), uintptr(flags), 0)
	success = r0 != 0
	if !success {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtNext(resultSet EvtHandle, eventArraySize uint32, eventArray *EvtHandle, timeout uint32, flags uint32, numReturned *uint32) (err error) {
	r1, _, e1 := syscall.Syscall6(procEvtNext.Addr(), 6, uintptr(resultSet), uintptr(eventArraySize), uintptr(unsafe.Pointer(eventArray)), uintptr(timeout), uintptr(flags), uintptr(unsafe.Pointer(numReturned)))
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtOpenChannelEnum(session EvtHandle, flags uint32) (handle EvtHandle, err error) {
	r0, _, e1 := syscall.Syscall(procEvtOpenChannelEnum.Addr(), 2, uintptr(session), uintptr(flags), 0)
	handle = EvtHandle(r0)
	if handle == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtNextChannelPath(channelEnum EvtHandle, channelPathBufferSize uint32, channelPathBuffer *uint16, channelPathBufferUsed *uint32) (err error) {
	r1, _, e1 := syscall.Syscall6(procEvtNextChannelPath.Addr(), 4, uintptr(channelEnum), uintptr(channelPathBufferSize), uintptr(unsafe.Pointer(channelPathBuffer)), uintptr(unsafe.Pointer(channelPathBufferUsed)), 0, 0)
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtFormatMessage(publisherMetadata EvtHandle, event EvtHandle, messageID uint32, valueCount uint32, values uintptr, flags EvtFormatMessageFlag, bufferSize uint32, buffer *byte, bufferUsed *uint32) (err error) {
	r1, _, e1 := syscall.Syscall9(procEvtFormatMessage.Addr(), 9, uintptr(publisherMetadata), uintptr(event), uintptr(messageID), uintptr(valueCount), uintptr(values), uintptr(flags), uintptr(bufferSize), uintptr(unsafe.Pointer(buffer)), uintptr(unsafe.Pointer(bufferUsed)))
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _EvtOpenPublisherMetadata(session EvtHandle, publisherIdentity *uint16, logFilePath *uint16, locale uint32, flags uint32) (handle EvtHandle, err error) {
	r0, _, e1 := syscall.Syscall6(procEvtOpenPublisherMetadata.Addr(), 5, uintptr(session), uintptr(unsafe.Pointer(publisherIdentity)), uintptr(unsafe.Pointer(logFilePath)), uintptr(locale), uintptr(flags), 0)
	handle = EvtHandle(r0)
	if handle == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func _StringFromGUID2(rguid *syscall.GUID, pStr *uint16, strSize uint32) (err error) {
	r1, _, e1 := syscall.Syscall(procStringFromGUID2.Addr(), 3, uintptr(unsafe.Pointer(rguid)), uintptr(unsafe.Pointer(pStr)), uintptr(strSize))
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}
