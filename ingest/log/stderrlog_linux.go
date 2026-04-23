//go:build linux
// +build linux

/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package log

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
)

func newStderrLogger(fileOverride string, cb StderrCallback) (lgr *Logger, err error) {
	var clr critLevelRelay
	if len(fileOverride) > 0 {
		clr.rfc = os.Stderr //this is getting redirected to a file
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
			clr.raw = os.NewFile(uintptr(oldstderr), "oldstderr") // this is going to the actual stderr
		}

		//dupe the output file onto stderr so that output goes there
		if err = syscall.Dup3(int(fout.Fd()), int(os.Stderr.Fd()), 0); err != nil {
			fout.Close()
		}

	} else {
		//just an rfc output
		clr.rfc = os.Stderr
	}
	lgr = NewLevelRelay(clr)
	return
}

type critLevelRelay struct {
	raw io.WriteCloser
	rfc io.WriteCloser
}

func (c critLevelRelay) WriteLog(l Level, ts time.Time, rfcline, rawline string) (err error) {
	if l >= ERROR {
		if c.raw != nil {
			if _, err = fmt.Fprintf(c.raw, "%s\n", rawline); err != nil {
				return
			}
		}
		if c.rfc != nil {
			if _, err = fmt.Fprintf(c.rfc, "%s\n", rfcline); err != nil {
				return
			}
		}
	}
	return
}

func (c critLevelRelay) Close() (err error) {
	if c.raw != nil {
		if lerr := c.raw.Close(); err != nil {
			err = lerr
		}
	}
	if c.rfc != nil {
		if lerr := c.rfc.Close(); err != nil {
			err = lerr
		}
	}
	return
}
