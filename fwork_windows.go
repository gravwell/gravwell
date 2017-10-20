/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// +build windows
package filewatch

import (
	"errors"
	"os"
	"syscall"
)

func getFileId(f *os.File) (id FileId, err error) {
	var bhfi syscall.ByHandleFileInformation
	h := syscall.Handle(f.Fd())
	if err = syscall.GetFileInformationByHandle(h, &bhfi); err != nil {
		return
	}
	id.Major = uint64(bhfi.VolumeSerialNumber)
	id.Minor = uint64(bhfi.FileIndexHigh) << 32
	id.Minor |= uint64(bhfi.FileIndexLow)
	return
}

func getFileIdFromName(name string) (id FileId, err error) {

	p, lerr := syscall.UTF16PtrFromString(name)
	if lerr != nil {
		err = lerr
		return
	}
	h, lerr := syscall.CreateFile(p, 0, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if lerr != nil {
		err = lerr
		return
	}
	defer syscall.CloseHandle(h)
	var bhfi syscall.ByHandleFileInformation
	if err = syscall.GetFileInformationByHandle(h, &bhfi); err != nil {
		return
	}
	id.Major = uint64(bhfi.VolumeSerialNumber)
	id.Minor = uint64(bhfi.FileIndexHigh) << 32
	id.Minor |= uint64(bhfi.FileIndexLow)
	return
}

// openDeletableFile is a wrapper which ensures that the open file
// can be deleted by other processes.  The windows version of this
// call passes in some additiona SHARE flags that the golang stdlib
// does not provide.  Which is why this wrapper even exists
func openDeletableFile(fpath string) (*os.File, error) {
	var attrib *syscall.SecurityAttributes
	if len(fpath) == 0 {
		return nil, errors.New("Empty file path, file not found")
	}

	p, err := syscall.UTF16PtrFromString(fpath)
	if err != nil {
		return nil, err
	}

	shared := uint32(syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE)
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, shared, attrib, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}

	return os.NewFile(uintptr(h), fpath), nil
}

// createDeletableFile is a wrapper which ensures that the open file
// can be deleted by other processes.  The windows version of this
// call passes in some additiona SHARE flags that the golang stdlib
// does not provide.  Which is why this wrapper even exists
func createDeletableFile(fpath string) (*os.File, error) {
	var attrib *syscall.SecurityAttributes
	if len(fpath) == 0 {
		return nil, errors.New("Empty file path, file not found")
	}

	p, err := syscall.UTF16PtrFromString(fpath)
	if err != nil {
		return nil, err
	}

	shared := uint32(syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE)
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, shared,
		attrib, syscall.CREATE_NEW|syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}

	return os.NewFile(uintptr(h), fpath), nil
}
