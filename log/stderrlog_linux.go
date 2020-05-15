/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package log

import (
	"os"
	"syscall"
)

func newStderrLogger(fileOverride string, cb StderrCallback) (lgr *Logger, err error) {
	var oldstderr int
	var fout *os.File
	lgr = New(os.Stderr)
	if len(fileOverride) > 0 {
		//get a handle on the output file
		if fout, err = os.Create(fileOverride); err != nil {
			return
		}
		if cb != nil {
			cb(fout)
		}

		//dup stderr
		if oldstderr, err = syscall.Dup(int(os.Stderr.Fd())); err != nil {
			fout.Close()
			return
		} else {
			lgr.AddWriter(os.NewFile(uintptr(oldstderr), "oldstderr"))
		}

		//dupe the output file onto stderr so that output goes there
		if err = syscall.Dup2(int(fout.Fd()), int(os.Stderr.Fd())); err != nil {
			fout.Close()
		}
	}
	return
}
