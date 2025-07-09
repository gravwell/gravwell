/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

var (
	tdir string
)

func TestMain(m *testing.M) {
	var err error
	if tdir, err = os.MkdirTemp(os.TempDir(), "state"); err != nil {
		fmt.Println("Failed to create temp dir", err)
		os.Exit(-1)
	}
	r := m.Run()
	if err = os.RemoveAll(tdir); err != nil {
		fmt.Println("Failed to remove tempdir", err)
		os.Exit(-1)
	}
	os.Exit(r)
}

type testS struct {
	Foo string
	Bar int
	Baz float64
}

func TestNewState(t *testing.T) {
	s, err := NewState(filepath.Join(tdir, "state1"), 0660)
	if err != nil {
		t.Fatal(err)
	}
	tv := testS{
		Foo: `test`,
		Bar: 1,
		Baz: 1.1,
	}
	var tv2 testS
	if err = s.Write(tv); err != nil {
		t.Fatal(err)
	}
	if err = s.Read(&tv2); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tv, tv2) {
		t.Fatal("Readout is wrong")
	}
}

func TestEmptyState(t *testing.T) {
	s, err := NewState(filepath.Join(tdir, "state2"), 0660)
	if err != nil {
		t.Fatal(err)
	}
	var tv2 testS
	if err = s.Read(&tv2); err != ErrNoState {
		fmt.Printf("%+v\n", tv2)
		t.Fatal("didn't get a bad state out of an empty set", err)
	}
}

func TestUpdates(t *testing.T) {
	wg := &sync.WaitGroup{}
	s, err := NewState(filepath.Join(tdir, "state3"), 0660)
	if err != nil {
		t.Fatal(err)
	}
	wg.Add(100)
	for i := 0; i < 100; i++ {
		go func(w *sync.WaitGroup, sm *State, v int) {
			tv := testS{
				Foo: `test`,
				Bar: v,
				Baz: 1.1,
			}
			if err = s.Write(tv); err != nil {
				t.Error(err)
			}
			wg.Done()
		}(wg, s, i)
	}
	wg.Wait()
	var tv testS
	if err = s.Read(&tv); err != nil {
		t.Fatal(err)
	}
	if tv.Foo != `test` || tv.Baz != 1.1 {
		t.Fatal("Readout is wrong")
	}
}
