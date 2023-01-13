/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
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

	"github.com/gravwell/gravwell/v3/client/types"
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
	pr.Size = uint64(len(pr.Data))
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
	if err == nil {
		pm.Expansion = string(bts)
	} else if os.IsNotExist(err) {
		err = nil
	}
	return
}

/**************************************************************************
 * User Files
 **************************************************************************/

func writeUserFile(dir string, name string, x types.UserFile) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "file")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop two files: .meta and .contents
	contentsPath := filepath.Join(p, fmt.Sprintf("%v.contents", name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	if err := ioutil.WriteFile(contentsPath, x.Contents, 0644); err != nil {
		return err
	}
	x.Contents = []byte{}
	mb, err := json.MarshalIndent(x, "", "	")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(metaPath, mb, 0644)
}

func readUserFile(dir, name string) (x types.UserFile, err error) {
	// Make sure the parent exists
	p := filepath.Join(dir, "file")
	contentsPath := filepath.Join(p, fmt.Sprintf("%v.contents", name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	// Read the metadata file first
	var bts []byte
	bts, err = ioutil.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x); err != nil {
		return
	}
	// Now read the contents and insert it
	bts, err = ioutil.ReadFile(contentsPath)
	if err == nil {
		x.Contents = bts
	} else if os.IsNotExist(err) {
		err = nil
	}
	return
}

/**************************************************************************
 * Search Library
 **************************************************************************/

func writeSearchLibrary(dir string, name string, x types.WireSearchLibrary) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "searchlibrary")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop two files: .meta and .query
	queryPath := filepath.Join(p, fmt.Sprintf("%v.query", name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	if err := ioutil.WriteFile(queryPath, []byte(x.Query), 0644); err != nil {
		return err
	}
	x.Query = ``
	mb, err := json.MarshalIndent(x.SearchLibrary, "", "	")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(metaPath, mb, 0644)
}

func readSearchLibrary(dir, name string) (x types.WireSearchLibrary, err error) {
	// Make sure the parent exists
	p := filepath.Join(dir, "searchlibrary")
	queryPath := filepath.Join(p, fmt.Sprintf("%v.query", name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	// Read the metadata file first
	var bts []byte
	bts, err = ioutil.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x); err != nil {
		return
	}
	// Now read the contents and insert it
	bts, err = ioutil.ReadFile(queryPath)
	if err == nil {
		x.Query = string(bts)
	} else if os.IsNotExist(err) {
		err = nil
	}
	return
}

/**************************************************************************
 * Extractors
 **************************************************************************/

func writeExtractor(dir string, name string, x types.AXDefinition) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "autoextractor")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop three files: .meta, .params, and .args
	paramsPath := filepath.Join(p, fmt.Sprintf("%v.params", name))
	argsPath := filepath.Join(p, fmt.Sprintf("%v.args", name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	if err := ioutil.WriteFile(paramsPath, []byte(x.Params), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(argsPath, []byte(x.Args), 0644); err != nil {
		return err
	}
	x.Params = ``
	x.Args = ``
	mb, err := json.MarshalIndent(x, "", "	")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(metaPath, mb, 0644)
}

func readExtractor(dir, name string) (x types.AXDefinition, err error) {
	// Make sure the parent exists
	p := filepath.Join(dir, "autoextractor")
	paramsPath := filepath.Join(p, fmt.Sprintf("%v.params", name))
	argsPath := filepath.Join(p, fmt.Sprintf("%v.args", name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	// Read the metadata file first
	var bts []byte
	bts, err = ioutil.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x); err != nil {
		return
	}
	// Now read the params and insert it
	bts, err = ioutil.ReadFile(paramsPath)
	if err == nil {
		x.Params = string(bts)
	} else if os.IsNotExist(err) {
		err = nil
	}
	bts, err = ioutil.ReadFile(argsPath)
	if err == nil {
		x.Args = string(bts)
	} else if os.IsNotExist(err) {
		err = nil
	}
	return
}

/**************************************************************************
 * Templates
 **************************************************************************/

func writeTemplate(dir string, name string, x types.PackedUserTemplate) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "template")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop two files: .meta and .query
	queryPath := filepath.Join(p, fmt.Sprintf("%v.query", name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	if err := ioutil.WriteFile(queryPath, []byte(x.Data.Query), 0644); err != nil {
		return err
	}
	x.Data.Query = ``
	mb, err := json.MarshalIndent(x, "", "	")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(metaPath, mb, 0644)
}

func readTemplate(dir, name string) (x types.PackedUserTemplate, err error) {
	// Make sure the parent exists
	p := filepath.Join(dir, "template")
	queryPath := filepath.Join(p, fmt.Sprintf("%v.query", name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	// Read the metadata file first
	var bts []byte
	bts, err = ioutil.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x); err != nil {
		return
	}
	// Now read the contents and insert it
	bts, err = ioutil.ReadFile(queryPath)
	if err == nil {
		x.Data.Query = string(bts)
	} else if os.IsNotExist(err) {
		err = nil
	}
	return
}

/**************************************************************************
 * Playbooks
 **************************************************************************/

func writePlaybook(dir string, name string, x types.Playbook) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "playbook")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop three files: .meta, .playbook_metadata, and .body
	bodyPath := filepath.Join(p, fmt.Sprintf("%v.body", name))
	pbMetaPath := filepath.Join(p, fmt.Sprintf("%v.playbook_metadata", name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	if err := ioutil.WriteFile(bodyPath, x.Body, 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(pbMetaPath, x.Metadata, 0644); err != nil {
		return err
	}
	// Now write out the rest to the meta file
	x.Body = []byte{}
	x.Metadata = []byte{}
	mb, err := json.MarshalIndent(x, "", "	")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(metaPath, mb, 0644)
}

func readPlaybook(dir, name string) (x types.Playbook, err error) {
	// Make sure the parent exists
	p := filepath.Join(dir, "playbook")
	bodyPath := filepath.Join(p, fmt.Sprintf("%v.body", name))
	pbMetaPath := filepath.Join(p, fmt.Sprintf("%v.playbook_metadata", name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	// Read the metadata file first
	var bts []byte
	bts, err = ioutil.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x); err != nil {
		return
	}
	// Now read the body and insert it
	bts, err = ioutil.ReadFile(bodyPath)
	if err == nil {
		x.Body = bts
	} else if os.IsNotExist(err) {
		err = nil
	} else {
		return
	}
	// And read the playbook_metadata file
	bts, err = ioutil.ReadFile(pbMetaPath)
	if err == nil {
		x.Metadata = bts
	} else if os.IsNotExist(err) {
		err = nil
	}

	return
}

/**************************************************************************
 * Scheduled Search
 **************************************************************************/

func writeScheduledSearch(dir string, name string, x kits.PackedScheduledSearch) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "scheduled")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop files: .meta, .search, .flow, and .script
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	searchPath := filepath.Join(p, fmt.Sprintf("%v.search", name))
	scriptPath := filepath.Join(p, fmt.Sprintf("%v.script", name))
	flowPath := filepath.Join(p, fmt.Sprintf("%v.flow", name))
	if err := ioutil.WriteFile(searchPath, []byte(x.SearchString), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(scriptPath, []byte(x.Script), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(flowPath, []byte(x.Flow), 0644); err != nil {
		return err
	}
	x.SearchString = ``
	x.Script = ``
	x.Flow = ``
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
	flowPath := filepath.Join(p, fmt.Sprintf("%v.flow", name))
	// Read the metadata file first
	var bts []byte
	bts, err = ioutil.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x); err != nil {
		return
	}
	// Now read search, flow, and script files
	bts, err = ioutil.ReadFile(searchPath)
	if err != nil {
		return
	}
	x.SearchString = string(bts)
	bts, err = ioutil.ReadFile(scriptPath)
	if err != nil {
		return
	}
	x.Script = string(bts)
	bts, err = ioutil.ReadFile(flowPath)
	if err != nil {
		// Flows are newer, so they might not exist in older stuff.
		if os.IsNotExist(err) {
			err = nil
		} else {
			return
		}
	}
	x.Flow = string(bts)
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
