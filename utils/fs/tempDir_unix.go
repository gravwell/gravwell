//go:build unix

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package fs

import (
	"os"
)

const (
	temporaryDirFallBack string = "/tmp/"
)

var tempDir = "/opt/gravwell/run/"

func init() {
	if f, err := os.Stat(tempDir); err != nil || !f.IsDir() {
		tempDir = temporaryDirFallBack
	}
}

func tempDirImpl() string {
	return tempDir
}
