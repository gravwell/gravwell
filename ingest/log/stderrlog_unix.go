//go:build linux || darwin
// +build linux darwin

/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package log

import (
	"io"
	"os"
	"syscall"
	"time"
)

func newStderrLogger(fileOverride string, cb StderrCallback) (lgr *Logger, err error) {
	var clr critLevelRelay
	if len(fileOverride) > 0 {
		var oldstderr int
		var fout *os.File
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
			clr.wc = []io.WriteCloser{os.NewFile(uintptr(oldstderr), "oldstderr")}
		}

		//dupe the output file onto stderr so that output goes there
		if err = syscall.Dup3(int(fout.Fd()), int(os.Stderr.Fd()), 0); err != nil {
			fout.Close()
		}
	}
	lgr = NewLevelRelay(clr)
	return
}

type critLevelRelay struct {
	wc []io.WriteCloser
}

func (c critLevelRelay) WriteLog(l Level, ts time.Time, rfcline, rawline string) (err error) {
	if l >= ERROR {
		if _, err = io.WriteString(os.Stderr, rawline); err != nil {
			return
		}
		for _, w := range c.wc {
			if _, err = io.WriteString(w, rawline); err != nil {
				return
			}
		}
	}
	return
}

func (c critLevelRelay) Close() (err error) {
	for _, wc := range c.wc {
		if lerr := wc.Close(); err != nil {
			err = lerr
		}
	}
	return
}
