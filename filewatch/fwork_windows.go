//go:build windows
// +build windows

/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package filewatch

import (
	"errors"
	"fmt"
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

// getFileIdFromName is a windows version of stat that returns something roughly equal to a device and inode.
// there is voodoo, we all hate it... yet here we are...
func getFileIdFromName(name string) (id FileId, err error) {
	p, lerr := syscall.UTF16PtrFromString(name)
	if lerr != nil {
		err = lerr
		return
	}

	//make sure to drop the share attributes even in this call because apparently
	//GetFileINformationByHandle syscall can take time... It looks like a stat...
	//it smells like a stat... it ain't a stat....
	shared := uint32(syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE)
	h, lerr := syscall.CreateFile(p, syscall.GENERIC_READ, shared, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if lerr != nil {
		err = fmt.Errorf("Failed to open %s to check FileID: %w", name, lerr)
		return
	}
	defer syscall.CloseHandle(h)
	var bhfi syscall.ByHandleFileInformation
	if err = syscall.GetFileInformationByHandle(h, &bhfi); err != nil {
		if os.IsNotExist(err) {
			err = os.ErrNotExist
		}
		err = &os.PathError{Op: "open", Path: name, Err: err}
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
	if len(fpath) == 0 {
		return nil, errors.New("Empty file path, file not found")
	}

	p, err := syscall.UTF16PtrFromString(fpath)
	if err != nil {
		return nil, err
	}

	shared := uint32(syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE)
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, shared, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		if os.IsNotExist(err) {
			err = os.ErrNotExist
		}
		return nil, &os.PathError{Op: "open", Path: fpath, Err: err}
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
		if os.IsNotExist(err) {
			err = os.ErrNotExist
		}
		return nil, &os.PathError{Op: "open", Path: fpath, Err: err}
	}

	return os.NewFile(uintptr(h), fpath), nil
}
