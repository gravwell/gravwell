/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/gravwell/gravwell/v3/client/types/kits"
)

/**************************************************************************
 * Resources
 **************************************************************************/

func writeResource(dir string, pr kits.PackedResource) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "resource")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop two files: .meta and .contents
	contentPath := filepath.Join(p, fmt.Sprintf("%v.contents", pr.ResourceName))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", pr.ResourceName))
	if err := ioutil.WriteFile(contentPath, pr.Data, 0644); err != nil {
		return err
	}
	pr.Data = []byte{}
	mb, err := json.MarshalIndent(pr, "", "	")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(metaPath, mb, 0644)
}

func readResource(dir string, name string) (pr kits.PackedResource, err error) {
	p := filepath.Join(dir, "resource")
	contentPath := filepath.Join(p, fmt.Sprintf("%v.contents", name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))

	// Read the metadata file first
	var bts []byte
	bts, err = ioutil.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &pr); err != nil {
		return
	}
	// Now read the contents into the resource
	pr.Data, err = ioutil.ReadFile(contentPath)
	hsh := md5.New()
	hsh.Write(pr.Data)
	pr.Hash = hsh.Sum(nil)
	return
}

/**************************************************************************
 * Macros
 **************************************************************************/

func writeMacro(dir string, pm kits.PackedMacro) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "macro")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop two files: .meta and .expansion
	expansionPath := filepath.Join(p, fmt.Sprintf("%v.expansion", pm.Name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", pm.Name))
	if err := ioutil.WriteFile(expansionPath, []byte(pm.Expansion), 0644); err != nil {
		return err
	}
	pm.Expansion = ``
	mb, err := json.MarshalIndent(pm, "", "	")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(metaPath, mb, 0644)
}

func readMacro(dir, name string) (pm kits.PackedMacro, err error) {
	// Make sure the parent exists
	p := filepath.Join(dir, "macro")
	expansionPath := filepath.Join(p, fmt.Sprintf("%v.expansion", name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	// Read the metadata file first
	var bts []byte
	bts, err = ioutil.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &pm); err != nil {
		return
	}
	// Now read the expansion and insert it
	bts, err = ioutil.ReadFile(expansionPath)
	pm.Expansion = string(bts)
	return
}

/**************************************************************************
 * Scheduled Search
 **************************************************************************/

func writeScheduledSearch(dir string, x kits.PackedScheduledSearch) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "scheduled")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop three files: .meta, .search, and .script
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", x.Name))
	searchPath := filepath.Join(p, fmt.Sprintf("%v.search", x.Name))
	scriptPath := filepath.Join(p, fmt.Sprintf("%v.script", x.Name))
	if err := ioutil.WriteFile(searchPath, []byte(x.SearchString), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(scriptPath, []byte(x.Script), 0644); err != nil {
		return err
	}
	x.SearchString = ``
	x.Script = ``
	mb, err := json.MarshalIndent(x, "", "	")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(metaPath, mb, 0644)
}

func readScheduledSearch(dir, name string) (x kits.PackedScheduledSearch, err error) {
	p := filepath.Join(dir, "scheduled")
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	searchPath := filepath.Join(p, fmt.Sprintf("%v.search", name))
	scriptPath := filepath.Join(p, fmt.Sprintf("%v.script", name))
	// Read the metadata file first
	var bts []byte
	bts, err = ioutil.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x); err != nil {
		return
	}
	// Now read search and script files
	bts, err = ioutil.ReadFile(searchPath)
	x.SearchString = string(bts)
	if err != nil {
		return
	}
	bts, err = ioutil.ReadFile(scriptPath)
	x.Script = string(bts)
	return
}

/**************************************************************************
 * Dashboard
 **************************************************************************/

func writeDashboard(dir string, name string, x kits.PackedDashboard) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "dashboard")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Just one file for now
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	mb, err := json.MarshalIndent(x, "", "	")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(metaPath, mb, 0644)
}

func readDashboard(dir, name string) (x kits.PackedDashboard, err error) {
	p := filepath.Join(dir, "dashboard")
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	// Read the metadata file
	var bts []byte
	bts, err = ioutil.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x); err != nil {
		return
	}
	return
}

/**************************************************************************
 * License
 **************************************************************************/

func writeLicense(dir string, name string, x []byte) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "license")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	lPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	return ioutil.WriteFile(lPath, x, 0644)
}

func readLicense(dir, name string) (x []byte, err error) {
	p := filepath.Join(dir, "license")
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))

	x, err = ioutil.ReadFile(metaPath)
	return
}

/**************************************************************************
 * Generic
 **************************************************************************/

func genericWrite(dir string, tp kits.ItemType, name string, x interface{}) error {
	// Make sure the parent exists
	p := filepath.Join(dir, tp.Ext())
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Just drop it all in a single file
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	mb, err := json.MarshalIndent(x, "", "	")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(metaPath, mb, 0644)
}

func genericRead(dir string, itm kits.Item, obj interface{}) (err error) {
	p := filepath.Join(dir, itm.Type.Ext())
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", itm.Name))
	// Read the metadata file
	var bts []byte
	bts, err = ioutil.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, obj); err != nil {
		return
	}
	return
}