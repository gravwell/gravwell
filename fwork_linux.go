/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// +build linux
package filewatch

import (
	"os"
	"syscall"
)

func getFileId(f *os.File) (id FileId, err error) {
	var sc syscall.Stat_t
	if err = syscall.Fstat(int(f.Fd()), &sc); err != nil {
		return
	}
	id.Major = sc.Dev
	id.Minor = sc.Ino
	return
}

func getFileIdFromName(name string) (id FileId, err error) {
	var sc syscall.Stat_t
	if err = syscall.Stat(name, &sc); err != nil {
		return
	}
	id.Major = sc.Dev
	id.Minor = sc.Ino
	return
}

// openDeletableFile is a wrapper which ensures that the open file
// can be deleted by other processes.  The Linux version of this
// call doesn't really do anything, as this functionality isn't
// restricted in POSIX systems
func openDeletableFile(fpath string) (*os.File, error) {
	return os.Open(fpath)
}

// createDeletableFile is a wrapper which ensures that the open file
// can be deleted by other processes.  The Linux version of this
// call doesn't really do anything, as this functionality isn't
// restricted in POSIX systems
func createDeletableFile(fpath string) (*os.File, error) {
	return os.Create(fpath)
}
