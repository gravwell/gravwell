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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testFile string = `test.log`
)

var (
	tempdir string
)

func TestMain(m *testing.M) {
	var err error
	if tempdir, err = ioutil.TempDir(os.TempDir(), ``); err != nil {
		fmt.Println("Failed to create temp dir", err)
		os.Exit(-1)
	}
	r := m.Run()
	os.RemoveAll(tempdir)
	os.Exit(r)
}

func newLogger() (*Logger, error) {
	p := filepath.Join(tempdir, testFile)
	fout, err := os.Create(p)
	if err != nil {
		return nil, err
	}
	return New(fout), nil
}

func appendLogger() (*Logger, error) {
	p := filepath.Join(tempdir, testFile)
	return NewFile(p)
}

func TestNew(t *testing.T) {
	lgr, err := newLogger()
	if err != nil {
		t.Fatal(err)
	}
	if err = lgr.Critical("test: %d", 99); err != nil {
		t.Fatal(err)
	}

	if err = lgr.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAppend(t *testing.T) {
	lgr, err := appendLogger()
	if err != nil {
		t.Fatal(err)
	}
	if err = lgr.Error("test: %d", 99); err != nil {
		t.Fatal(err)
	}

	if err = lgr.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestValue(t *testing.T) {
	lgr, err := appendLogger()
	if err != nil {
		t.Fatal(err)
	}
	if err = lgr.Warn("test: %d", 99); err != nil {
		t.Fatal(err)
	}
	if err = lgr.Info("test: %d\n", 99); err != nil {
		t.Fatal(err)
	}
	if err = lgr.Debug("test: %d", 99); err != nil {
		t.Fatal(err)
	}
	if err = lgr.SetLevel(OFF); err != nil {
		t.Fatal(err)
	}
	if err = lgr.Critical("testing off: %d", 88); err != nil {
		t.Fatal(err)
	}
	if err = lgr.Close(); err != nil {
		t.Fatal(err)
	}
	bts, err := ioutil.ReadFile(filepath.Join(tempdir, testFile))
	if err != nil {
		t.Fatal(err)
	}
	s := string(bts)
	if !strings.Contains(s, "CRITICAL test: 99\n") {
		t.Fatal("Missing critical value: ", s)
	}
	if !strings.Contains(s, "ERROR test: 99\n") {
		t.Fatal("Missing critical value: ", s)
	}
	if !strings.Contains(s, "WARN test: 99\n") {
		t.Fatal("Missing critical value: ", s)
	}
	if !strings.Contains(s, "INFO test: 99\n") {
		t.Fatal("Missing critical value: ", s)
	}
	if strings.Contains(s, "DEBUG test: 99\n") {
		t.Fatal("Has debug level: ", s)
	}
	if strings.Contains(s, "CRITICAL testing off: 88\n") {
		t.Fatal("Has CRITICAL level: ", s)
	}
	if strings.Contains(s, "\n\n") {
		t.Fatal("did not filter double newlines")
	}
}

func TestMulti(t *testing.T) {
	lgr, err := newLogger()
	if err != nil {
		t.Fatal(err)
	}
	var toCheck []string
	for i := 0; i < 8; i++ {
		fout, err := ioutil.TempFile(tempdir, ``)
		if err != nil {
			t.Fatal(err)
		}
		if err = lgr.AddWriter(fout); err != nil {
			t.Fatal(err)
		}
		toCheck = append(toCheck, fout.Name())
	}

	if err = lgr.Critical("0x%x", 0x1337); err != nil {
		t.Fatal(err)
	}

	if err = lgr.Error("test %d", 1337); err != nil {
		t.Fatal(err)
	}
	for _, n := range toCheck {
		bts, err := ioutil.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(bts), "CRITICAL 0x1337\n") {
			t.Fatal(n, " missing critical log value")
		}
		if !strings.Contains(string(bts), "ERROR test 1337\n") {
			t.Fatal(n, " missing error log value ")
		}
	}
	if err = lgr.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAddRemove(t *testing.T) {
	lgr, err := newLogger()
	if err != nil {
		t.Fatal(err)
	}
	var added []io.WriteCloser
	var toCheck []string
	for i := 0; i < 8; i++ {
		fout, err := ioutil.TempFile(tempdir, ``)
		if err != nil {
			t.Fatal(err)
		}
		if err = lgr.AddWriter(fout); err != nil {
			t.Fatal(err)
		}
		defer fout.Close()
		added = append(added, fout)
		toCheck = append(toCheck, fout.Name())
	}

	if err = lgr.Critical("0x%x", 0x1337); err != nil {
		t.Fatal(err)
	}

	//remove all the added items
	for i := range added {
		if err = lgr.DeleteWriter(added[i]); err != nil {
			t.Fatal(err)
		}
	}

	//log something that should ONLY go to the tempdir
	if err = lgr.Error("test %d", 1337); err != nil {
		t.Fatal(err)
	}

	for _, n := range toCheck {
		bts, err := ioutil.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(bts), "CRITICAL 0x1337\n") {
			t.Fatal(n, " missing critical log value")
		}
		if strings.Contains(string(bts), "ERROR test 1337\n") {
			t.Fatal(n, " contains values it should not")
		}
	}

	//check the original which should have both
	bts, err := ioutil.ReadFile(filepath.Join(tempdir, testFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bts), "CRITICAL 0x1337\n") {
		t.Fatal("original missing critical log value")
	}
	if !strings.Contains(string(bts), "ERROR test 1337\n") {
		t.Fatal("original missing ERROR values")
	}

	if err = lgr.Close(); err != nil {
		t.Fatal(err)
	}
}
