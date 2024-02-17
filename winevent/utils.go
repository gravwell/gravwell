//go:build windows
// +build windows

/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package winevent

import (
	"fmt"
	"os"
	"path/filepath"
)

func ServiceFilename(name string) (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return ``, fmt.Errorf("Failed to get executable path: %v", err)
	}
	exeDir, err := filepath.Abs(filepath.Dir(exePath))
	if err != nil {
		return ``, fmt.Errorf("Failed to get location of executable: %v", err)
	}
	return filepath.Join(exeDir, name), nil
}

func ProgramDataFilename(name string) (r string, err error) {
	if r = os.Getenv(`PROGRAMDATA`); r == `` {
		//return the ServiceFilename path
		r, err = ServiceFilename(name)
	} else {
		r = filepath.Join(r, name)
	}
	return
}
