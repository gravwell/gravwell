/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package filewatch

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"testing"
)

const (
	//defMaxLine    int    = 4096
	defMaxLine    int = 32
	testLineCount int = 1024
)

var (
	lastIndex   int64
	testingBase = os.TempDir()
)

func TestLinerNew(t *testing.T) {
	f, name, err := newFile()
	if err != nil {
		t.Fatal(err)
	}

	lr, err := NewLineReader(f, defMaxLine, lastIndex)
	if err != nil {
		t.Fatal(err)
	}

	if err := lr.Close(); err != nil {
		t.Fatal(err)
	}

	cleanFile(name, t)
}

func newLiner() (lnr *LineReader, name string, err error) {
	var f *os.File
	f, name, err = newFile()
	if err != nil {
		return
	}

	lnr, err = NewLineReader(f, defMaxLine, lastIndex)
	if err != nil {
		f.Close()
		os.RemoveAll(name)
		return
	}
	return
}

func TestLinerInput(t *testing.T) {
	lnr, name, err := newLiner()
	if err != nil {
		t.Fatal(err)
	}

	cnt, lines, err := writeLines(name)
	if err != nil {
		lnr.Close()
		cleanFile(name, t)
		t.Fatal(err)
	}

	//read lines until we can't
	var count int
	for {
		ln, ok, _, err := lnr.ReadEntry()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if _, ok := lines[string(ln)]; !ok {
			t.Fatal("Could not find", ln, "in oracle set")
		}
		count++
	}
	if count != cnt {
		t.Fatal(fmt.Errorf("Failed to read all lines: %d != %d", count, len(lines)))
	}

	if err := lnr.Close(); err != nil {
		t.Fatal(err)
	}
	cleanFile(name, t)
}

func TestLinerContinuousInput(t *testing.T) {
	lnr, name, err := newLiner()
	if err != nil {
		t.Fatal(err)
	}

	cnt, lines, err := writeLines(name)
	if err != nil {
		lnr.Close()
		cleanFile(name, t)
		t.Fatal(err)
	}

	//read lines until we can't
	var count int
	for {
		ln, ok, _, err := lnr.ReadEntry()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if _, ok := lines[string(ln)]; !ok {
			t.Fatal("Could not find", ln, "in oracle set")
		}
		count++
	}

	//write another set of lines
	cnt2, lines2, err := writeLines(name)
	if err != nil {
		lnr.Close()
		cleanFile(name, t)
		t.Fatal(err)
	}

	for {
		ln, ok, _, err := lnr.ReadEntry()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if _, ok := lines2[string(ln)]; !ok {
			t.Fatal("Could not find", string(ln), "in oracle set")
		}
		count++
	}

	if count != (cnt + cnt2) {
		t.Fatal(fmt.Errorf("Failed to read all lines: %d != %d", count, cnt+cnt2))
	}

	if err := lnr.Close(); err != nil {
		t.Fatal(err)
	}
	cleanFile(name, t)
}

func newFile() (f *os.File, name string, err error) {
	f, err = ioutil.TempFile(testingBase, `liner`)
	if err != nil {
		return
	}
	name = f.Name()
	if err = f.Close(); err != nil {
		return
	}
	f, err = createDeletableFile(name)
	return
}

func cleanFile(nm string, t *testing.T) {
	if err := os.RemoveAll(nm); err != nil {
		t.Fatal(err)
	}
}

func writeLines(fname string) (int, map[string]bool, error) {
	fout, err := os.OpenFile(fname, os.O_WRONLY|os.O_CREATE, 0660)
	if err != nil {
		return 0, nil, err
	}
	//seek to the end of the file
	if _, err := fout.Seek(0, 2); err != nil {
		return 0, nil, err
	}
	mp := map[string]bool{}

	var count int
	var s string
	for i := 0; i < testLineCount; i++ {
		b := randomString(defMaxLine)
		s = string(b)
		fmt.Fprintf(fout, "%s\r\n", s)
		if len(b) > 0 {
			mp[s] = true
			count++
		}
	}

	if err := fout.Close(); err != nil {
		return 0, nil, err
	}
	return count, mp, nil
}

func randomString(n int) []byte {
	var set = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01234567890")
	buff := make([]byte, rand.Intn(n))
	for i := range buff {
		buff[i] = set[rand.Intn(len(set))]
	}
	return buff
}
