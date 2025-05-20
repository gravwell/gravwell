/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package chancacher

import (
	"os"
	"testing"
)

func TestFileCounter(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "testfilecounter")
	if err != nil {
		t.Errorf("tempfile: %v", err)
		t.FailNow()
	}
	defer f.Close()
	defer os.Remove(f.Name())

	fc, _ := NewFileCounter(f)

	data := []byte{'1', '2', '3', '4', '5'}

	n, err := fc.Write(data)
	if err != nil {
		t.Errorf("could not write data: %v", err)
		t.FailNow()
	}
	if n != 5 {
		t.Errorf("could not write enough data: %v", n)
	}

	if fc.Count() != 5 {
		t.Errorf("count mismatch: %v != 5", fc.Count())
	}

	rdata := make([]byte, 10)

	fc.Seek(0, 0)
	n, err = fc.Read(rdata)
	if err != nil {
		t.Errorf("could not read data: %v", err)
		t.FailNow()
	}
	if n != 5 {
		t.Errorf("could not read enough data: %v", n)
	}

	if fc.Count() != 0 {
		t.Errorf("count mismatch: %v != 0", fc.Count())
	}

}

func TestFileCounterCount(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "testfilecounter")
	if err != nil {
		t.Errorf("tempfile: %v", err)
		t.FailNow()
	}
	defer f.Close()
	defer os.Remove(f.Name())

	fc, _ := NewFileCounter(f)

	data := []byte{'1', '2', '3', '4', '5'}

	n, err := fc.Write(data)
	if err != nil {
		t.Errorf("could not write data: %v", err)
		t.FailNow()
	}
	if n != 5 {
		t.Errorf("could not write enough data: %v", n)
	}

	if fc.Count() != 5 {
		t.Errorf("count mismatch: %v != 5", fc.Count())
	}

	// Now re-open
	f2, err := os.Open(f.Name())
	if err != nil {
		t.Errorf("Open: %v", err)
		t.FailNow()
	}
	defer f2.Close()
	fc, _ = NewFileCounter(f2)

	if fc.Count() != 5 {
		t.Errorf("count mismatch: %v != 0", fc.Count())
	}

}

func TestFileCounterNil(t *testing.T) {
	var f *fileCounter
	f = nil
	c := f.Count()
	if c != 0 {
		t.Errorf("Count should be 0, got %v", c)
		t.FailNow()
	}
	f = &fileCounter{}
	c = f.Count()
	if c != 0 {
		t.Errorf("Count should be 0, got %v", c)
		t.FailNow()
	}
}
