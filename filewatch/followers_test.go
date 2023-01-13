/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package filewatch

import (
	"errors"
	"os"
	"testing"
	"time"
)

const (
	baseName    string = `testing`
	altBaseName string = `niner`

	movePath string = `/tmp/follower_test.log.tmp`
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
	fcfg := FollowerConfig{
		BaseName: baseName,
		FilePath: fname,
		State:    &fstate,
		FilterID: 0,
		Handler:  &clh,
	}
	fl, err := NewFollower(fcfg)
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

	fcfg := FollowerConfig{
		BaseName: baseName,
		FilePath: fname,
		State:    &fstate,
		FilterID: 0,
		Handler:  &clh,
	}
	fl, err := NewFollower(fcfg)
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

func testStart(b, f string, tlh *trackingLH, fPtr *int64) (fl *follower, err error) {
	fcfg := FollowerConfig{
		BaseName: b,
		FilePath: f,
		State:    fPtr,
		FilterID: 0,
		Handler:  tlh,
	}
	if fl, err = NewFollower(fcfg); err != nil {
		os.RemoveAll(f)
		return
	}

	if err = fl.Start(); err != nil {
		os.RemoveAll(f)
		return
	}
	return
}

func waitForStop(fl *follower, tlh *trackingLH, l int) error {
	//up to 1 second for it to stop
	var i int
	//wait for it to actually quit
	for i = 0; i < 100; i++ {
		if l == len(tlh.mp) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if i >= 100 {
		return errors.New("Timed out while waiting for follower to get all the lines")
	}

	if err := fl.Stop(); err != nil {
		return err
	}
	return nil
}

func TestFeeder(t *testing.T) {
	var tlh trackingLH
	var state int64
	fname, err := newFileName()
	if err != nil {
		t.Fatal(err)
	}
	fl, err := testStart(baseName, fname, &tlh, &state)
	if err != nil {
		os.RemoveAll(fname)
		t.Fatal(err)
	}

	_, mp, err := writeLines(fname)
	if err != nil {
		fl.Close()
		os.RemoveAll(fname)
		t.Fatal(err)
	}

	if err := waitForStop(fl, &tlh, len(mp)); err != nil {
		os.RemoveAll(fname)
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

func TestMove(t *testing.T) {
	var tlh trackingLH
	var state int64
	fname, err := newFileName()
	if err != nil {
		t.Fatal(err)
	}
	fl, err := testStart(baseName, fname, &tlh, &state)
	if err != nil {
		os.RemoveAll(fname)
		t.Fatal(err)
	}

	_, mp, err := writeLines(fname)
	if err != nil {
		fl.Close()
		os.RemoveAll(fname)
		t.Fatal(err)
	}

	//move the file to /tmp/
	if err := os.Rename(fname, movePath); err != nil {
		os.RemoveAll(fname)
		t.Fatal(err)
	}
	defer os.RemoveAll(movePath)
	time.Sleep(10 * time.Millisecond)

	if err := waitForStop(fl, &tlh, len(mp)); err != nil {
		os.RemoveAll(fname)
		t.Fatal(err)
	}

	if err := fl.Close(); err != nil {
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
	testTagger
	cnt int64
}

func (h *countingLH) HandleLog(b []byte, ts time.Time) error {
	if len(b) > 0 && !ts.IsZero() {
		h.cnt++
	}
	return nil
}

type trackingLH struct {
	testTagger
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

type testTagger struct{}

func (tt testTagger) Tag() string {
	return `default`
}
