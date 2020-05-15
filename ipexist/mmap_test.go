/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
package ipexist

import (
	"io/ioutil"
	"testing"
)

func TestNewMMap(t *testing.T) {
	f, err := ioutil.TempFile(testDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	fm, err := MapFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := fm.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExpand(t *testing.T) {
	f, err := ioutil.TempFile(testDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	fm, err := MapFile(f)
	if err != nil {
		t.Fatal(err)
	}

	buff := make([]byte, pageSize*16)
	if n, err := f.Write(buff); err != nil {
		t.Fatal(err)
	} else if n != len(buff) {
		t.Fatal("failed write")
	}
	if err := fm.Expand(); err != nil {
		t.Fatal(err)
	}
	if fm.Size() != int64(len(buff)) {
		t.Fatal("Map did not expand properly", fm.Size(), len(buff))
	}

	//expand with something that is aligned poorly
	oldsz := len(buff)
	buff = buff[0:21]
	if n, err := f.Write(buff); err != nil {
		t.Fatal(err)
	} else if n != len(buff) {
		t.Fatal("failed write")
	}
	if err := fm.Expand(); err != nil {
		t.Fatal(err)
	}
	if fm.Size() != int64(len(buff)+oldsz) {
		t.Fatal("Map did not expand properly", fm.Size(), len(buff)+oldsz)
	}

	if err := fm.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSetSize(t *testing.T) {
	f, err := ioutil.TempFile(testDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	fm, err := MapFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := fm.SetSize(0x10001); err != nil {
		t.Fatal(err)
	}
	if fm.Size() != int64(0x10001) {
		t.Fatal("Map did not expand properly", fm.Size(), 0x10001)
	}

	if err := fm.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreload(t *testing.T) {
	f, err := ioutil.TempFile(testDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	fm, err := MapFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := fm.SetSize(0x10000); err != nil {
		t.Fatal(err)
	}
	if fm.Size() != int64(0x10000) {
		t.Fatal("Map did not expand properly", fm.Size(), 0x10000)
	}

	if err := fm.PreloadFile(); err != nil {
		t.Fatal(err)
	}

	if err := fm.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWrite(t *testing.T) {
	f, err := ioutil.TempFile(testDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	fname := f.Name()
	fm, err := MapFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := fm.SetSize(0x10000); err != nil {
		t.Fatal(err)
	}
	if fm.Size() != int64(0x10000) {
		t.Fatal("Map did not expand properly", fm.Size(), 0x10000)
	}
	for i := range fm.Buff {
		fm.Buff[i] = 0xFF
	}
	if err := fm.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	tbuff, err := ioutil.ReadFile(fname)
	if err != nil {
		t.Fatal(err)
	}
	if len(tbuff) != 0x10000 {
		t.Fatal("Bad file size", len(tbuff))
	}
	for i, v := range tbuff {
		if v != 0xff {
			t.Fatalf("byte %x is bad %x", i, v)
		}
	}
}
