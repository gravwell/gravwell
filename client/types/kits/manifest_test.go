/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package kits

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
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
	baseDir, err = os.MkdirTemp(os.TempDir(), "gravmanifest")
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
	m.Add(types.KitItem{
		Name: `foo`,
		ID:   uuid.New().String(),
		Type: types.KitAssetScheduledSearch,
	})

	iconID := uuid.New().String()
	iconFile := types.KitItem{
		Name: iconID,
		ID:   iconID,
		Type: types.KitAssetFile,
	}
	//try setting it when we haven't added the icon file yet
	if err := m.SetIcon(iconFile.ID); err == nil {
		t.Fatal("Failed to catch missing icon on setting")
	}
	//add it and try again
	if err := m.Add(iconFile); err != nil {
		t.Fatal(err)
	}
	if err := m.SetIcon(iconFile.ID); err != nil {
		t.Fatal(err)
	}
}

func TestMarshal(t *testing.T) {
	a := types.KitItem{
		Name: `foo`,
		ID:   uuid.New().String(),
		Type: types.KitAssetScheduledSearch,
	}
	for i := range a.Hash {
		a.Hash[i] = byte(i)
	}
	bts, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var b types.KitItem
	if err = json.Unmarshal(bts, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("bad marshal unmarshal")
	}
}
