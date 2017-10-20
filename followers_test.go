/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package filewatch

import (
	"os"
	"testing"
	"time"
)

const (
	baseName    string = `testing`
	altBaseName string = `niner`
)

var (
	fstate int64
)

func TestNewFollower(t *testing.T) {
	var clh countingLH
	fname, err := newFileName()
	if err != nil {
		t.Fatal(err)
	}
	fl, err := NewFollower(baseName, fname, &fstate, 0, &clh)
	if err != nil {
		cleanFile(fname, t)
		t.Fatal(err)
	}

	if err := fl.Close(); err != nil {
		cleanFile(fname, t)
		t.Fatal(err)
	}

	cleanFile(fname, t)
}

func TestNewStartStop(t *testing.T) {
	var clh countingLH
	fname, err := newFileName()
	if err != nil {
		t.Fatal(err)
	}
	fl, err := NewFollower(baseName, fname, &fstate, 0, &clh)
	if err != nil {
		cleanFile(fname, t)
		t.Fatal(err)
	}

	if err := fl.Start(); err != nil {
		cleanFile(fname, t)
		t.Fatal(err)
	}

	if err := fl.Stop(); err != nil {
		cleanFile(fname, t)
		t.Fatal(err)
	}

	if err := fl.Close(); err != nil {
		cleanFile(fname, t)
		t.Fatal(err)
	}

	cleanFile(fname, t)
}

func TestFeeder(t *testing.T) {
	var tlh trackingLH
	fname, err := newFileName()
	if err != nil {
		t.Fatal(err)
	}
	fl, err := NewFollower(baseName, fname, &fstate, 0, &tlh)
	if err != nil {
		if err := os.RemoveAll(fname); err != nil {
			t.Fatal(err)
		}
		t.Fatal(err)
	}

	if err := fl.Start(); err != nil {
		if err := os.RemoveAll(fname); err != nil {
			t.Fatal(err)
		}
		t.Fatal(err)
	}

	_, mp, err := writeLines(fname)
	if err != nil {
		fl.Close()
		if err := os.RemoveAll(fname); err != nil {
			t.Fatal(err)
		}
		t.Fatal(err)
	}

	//up to 1 second for it to stop
	var i int
	//wait for it to actually quit
	for i = 0; i < 100; i++ {
		if len(mp) == len(tlh.mp) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if i >= 100 {
		t.Fatal("Timed out while waiting for follower to get all the lines")
	}

	if err := fl.Stop(); err != nil {
		if err := os.RemoveAll(fname); err != nil {
			t.Fatal(err)
		}
		t.Fatal(err)
	}

	if err := fl.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(fname); err != nil {
		t.Fatal(err)
	}

	for k := range mp {
		if _, ok := tlh.mp[k]; !ok {
			t.Fatal("Failed to get all lines out")
		}
	}
}

func newFileName() (string, error) {
	f, name, err := newFile()
	if err != nil {
		return ``, err
	}
	if err := f.Close(); err != nil {
		return ``, err
	}
	return name, nil
}

type countingLH struct {
	cnt int64
}

func (h *countingLH) HandleLog(b []byte, ts time.Time) error {
	if len(b) > 0 && !ts.IsZero() {
		h.cnt++
	}
	return nil
}

type trackingLH struct {
	mp map[string]time.Time
}

func (h *trackingLH) HandleLog(b []byte, ts time.Time) error {
	if h.mp == nil {
		h.mp = map[string]time.Time{}
	}
	if len(b) > 0 {
		h.mp[string(b)] = ts
	}
	return nil
}
