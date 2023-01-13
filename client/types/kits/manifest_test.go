/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package kits

import (
	"flag"
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

const (
	privKeyFileName string = `priv.key`
	pubKeyFileName  string = `pub.key`
)

var (
	baseDir string
)

func TestMain(m *testing.M) {
	var err error
	flag.Parse() // so we can get the flags
	baseDir, err = ioutil.TempDir(os.TempDir(), "gravmanifest")
	if !testing.Short() {
		if err != nil {
			log.Fatal(err)
		}
	}
	r := m.Run()
	if err := os.RemoveAll(baseDir); err != nil {
		log.Fatal(err)
	}
	os.Exit(r)
}

func TestAddIcon(t *testing.T) {
	m := Manifest{Version: Version}
	// add some garbage
	m.Add(Item{
		Name: `foo`,
		Type: 2,
	})

	iconFile := Item{
		Name: uuid.New().String(),
		Type: File,
	}
	//try setting it when we haven't added the icon file yet
	if err := m.SetIcon(iconFile.Name); err == nil {
		t.Fatal("Failed to catch missing icon on setting")
	}
	//add it and try again
	if err := m.Add(iconFile); err != nil {
		t.Fatal(err)
	}
	if err := m.SetIcon(iconFile.Name); err != nil {
		t.Fatal(err)
	}
}

func TestMarshal(t *testing.T) {
	a := Item{
		Name: `foo`,
		Type: 2,
	}
	for i := range a.Hash {
		a.Hash[i] = byte(i)
	}
	bts, err := a.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var b Item
	if err = b.UnmarshalJSON(bts); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("bad marshal unmarshal")
	}
}
