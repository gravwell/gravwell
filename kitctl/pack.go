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
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"

	"encoding/hex"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/client/types/kits"
	"github.com/gravwell/gravwell/v4/utils/jsoncompat"
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
	// We use the name because for resources, it's guaranteed to be unique.
	contentPath := filepath.Join(p, fmt.Sprintf("%v.contents", pr.Name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", pr.Name))
	if err := os.WriteFile(contentPath, pr.Data, 0644); err != nil {
		return err
	}
	pr.Data = []byte{}
	mb, err := json.Marshal(pr, jsoncompat.Options, jsontext.WithIndentPrefix(""), jsontext.WithIndent("	"))
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, mb, 0644)
}

func readResource(dir string, name string) (pr kits.PackedResource, err error) {
	p := filepath.Join(dir, "resource")
	contentPath := filepath.Join(p, fmt.Sprintf("%v.contents", name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))

	// Read the metadata file first
	var bts []byte
	bts, err = os.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &pr, jsoncompat.Options); err != nil {
		return
	}
	// Now read the contents into the resource
	pr.Data, err = os.ReadFile(contentPath)
	hsh := md5.New()
	hsh.Write(pr.Data)
	pr.Hash = hex.EncodeToString(hsh.Sum(nil))
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
	// Name is guaranteed unique, so use it
	expansionPath := filepath.Join(p, fmt.Sprintf("%v.expansion", pm.Name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", pm.Name))
	if err := os.WriteFile(expansionPath, []byte(pm.Expansion), 0644); err != nil {
		return err
	}
	pm.Expansion = ``
	mb, err := json.Marshal(pm, jsoncompat.Options, jsontext.WithIndentPrefix(""), jsontext.WithIndent("	"))
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, mb, 0644)
}

func readMacro(dir, name string) (pm kits.PackedMacro, err error) {
	// Make sure the parent exists
	p := filepath.Join(dir, "macro")
	expansionPath := filepath.Join(p, fmt.Sprintf("%v.expansion", name))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))
	// Read the metadata file first
	var bts []byte
	bts, err = os.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &pm, jsoncompat.Options); err != nil {
		return
	}
	// Now read the expansion and insert it
	bts, err = os.ReadFile(expansionPath)
	if err == nil {
		pm.Expansion = string(bts)
	} else if os.IsNotExist(err) {
		err = nil
	}
	return
}

/**************************************************************************
 * Files
 **************************************************************************/

func writeFile(dir string, x kits.PackedFile) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "file")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop two files: .meta and .contents, using ID as item name
	contentsPath := filepath.Join(p, fmt.Sprintf("%v.contents", x.ID))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", x.ID))
	if err := os.WriteFile(contentsPath, x.Data, 0644); err != nil {
		return err
	}
	x.Data = []byte{}
	mb, err := json.Marshal(x, jsoncompat.Options, jsontext.WithIndentPrefix(""), jsontext.WithIndent("	"))
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, mb, 0644)
}

func readFile(dir, id string) (pf kits.PackedFile, err error) {
	// Make sure the parent exists
	p := filepath.Join(dir, "file")
	contentPath := filepath.Join(p, fmt.Sprintf("%v.contents", id))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))

	// Read the metadata file first
	var bts []byte
	bts, err = os.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &pf, jsoncompat.Options); err != nil {
		return
	}
	// Now read the contents into the file
	pf.Data, err = os.ReadFile(contentPath)
	hsh := md5.New()
	hsh.Write(pf.Data)
	pf.Hash = hex.EncodeToString(hsh.Sum(nil))
	pf.Size = uint64(len(pf.Data))
	return
}

/**************************************************************************
 * Search Library
 **************************************************************************/

func writeSearchLibrary(dir string, id string, x kits.PackedSavedQuery) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "searchlibrary")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop two files: .meta and .query
	queryPath := filepath.Join(p, fmt.Sprintf("%v.query", id))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	if err := os.WriteFile(queryPath, []byte(x.Query), 0644); err != nil {
		return err
	}
	x.Query = ``
	mb, err := json.Marshal(x, jsoncompat.Options, jsontext.WithIndentPrefix(""), jsontext.WithIndent("	"))
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, mb, 0644)
}

func readSearchLibrary(dir, id string) (x kits.PackedSavedQuery, err error) {
	// Make sure the parent exists
	p := filepath.Join(dir, "searchlibrary")
	queryPath := filepath.Join(p, fmt.Sprintf("%v.query", id))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	// Read the metadata file first
	var bts []byte
	bts, err = os.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x, jsoncompat.Options); err != nil {
		return
	}
	// Now read the contents and insert it
	bts, err = os.ReadFile(queryPath)
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

func writeExtractor(dir string, id string, x kits.PackedAX) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "autoextractor")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop three files: .meta, .params, and .args
	paramsPath := filepath.Join(p, fmt.Sprintf("%v.params", id))
	argsPath := filepath.Join(p, fmt.Sprintf("%v.args", id))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	if err := os.WriteFile(paramsPath, []byte(x.Params), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(argsPath, []byte(x.Args), 0644); err != nil {
		return err
	}
	x.Params = ``
	x.Args = ``
	mb, err := json.Marshal(x, jsoncompat.Options, jsontext.WithIndentPrefix(""), jsontext.WithIndent("	"))
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, mb, 0644)
}

func readExtractor(dir, id string) (x kits.PackedAX, err error) {
	// Make sure the parent exists
	p := filepath.Join(dir, "autoextractor")
	paramsPath := filepath.Join(p, fmt.Sprintf("%v.params", id))
	argsPath := filepath.Join(p, fmt.Sprintf("%v.args", id))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	// Read the metadata file first
	var bts []byte
	bts, err = os.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x, jsoncompat.Options); err != nil {
		return
	}
	// Now read the params and insert it
	bts, err = os.ReadFile(paramsPath)
	if err == nil {
		x.Params = string(bts)
	} else if os.IsNotExist(err) {
		err = nil
	}
	bts, err = os.ReadFile(argsPath)
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

func writeTemplate(dir string, id string, x kits.PackedUserTemplate) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "template")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop two files: .meta and .query
	queryPath := filepath.Join(p, fmt.Sprintf("%v.query", id))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	if err := os.WriteFile(queryPath, []byte(x.Query), 0644); err != nil {
		return err
	}
	x.Query = ``
	mb, err := json.Marshal(x, jsoncompat.Options, jsontext.WithIndentPrefix(""), jsontext.WithIndent("	"))
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, mb, 0644)
}

func readTemplate(dir, id string) (x kits.PackedUserTemplate, err error) {
	// Make sure the parent exists
	p := filepath.Join(dir, "template")
	queryPath := filepath.Join(p, fmt.Sprintf("%v.query", id))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	// Read the metadata file first
	var bts []byte
	bts, err = os.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x, jsoncompat.Options); err != nil {
		return
	}
	// Now read the contents and insert it
	bts, err = os.ReadFile(queryPath)
	if err == nil {
		x.Query = string(bts)
	} else if os.IsNotExist(err) {
		err = nil
	}
	return
}

/**************************************************************************
 * Playbooks
 **************************************************************************/

func writePlaybook(dir string, id string, x kits.PackedPlaybook) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "playbook")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop two files: .meta and .body
	bodyPath := filepath.Join(p, fmt.Sprintf("%v.body", id))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	if err := os.WriteFile(bodyPath, []byte(x.Body), 0644); err != nil {
		return err
	}
	// Now write out the rest to the meta file
	x.Body = ``
	mb, err := json.Marshal(x, jsoncompat.Options, jsontext.WithIndentPrefix(""), jsontext.WithIndent("	"))
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, mb, 0644)
}

func readPlaybook(dir, id string) (x kits.PackedPlaybook, err error) {
	// Make sure the parent exists
	p := filepath.Join(dir, "playbook")
	bodyPath := filepath.Join(p, fmt.Sprintf("%v.body", id))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	// Read the metadata file first
	var bts []byte
	bts, err = os.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x, jsoncompat.Options); err != nil {
		return
	}
	// Now read the body and insert it
	bts, err = os.ReadFile(bodyPath)
	if err == nil {
		x.Body = string(bts)
	} else if os.IsNotExist(err) {
		err = nil
	} else {
		return
	}

	return
}

/**************************************************************************
 * Scheduled Search
 **************************************************************************/

func writeScheduledSearch(dir string, id string, x kits.PackedScheduledSearch) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "scheduled")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop files
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	searchPath := filepath.Join(p, fmt.Sprintf("%v.search", id))
	if err := os.WriteFile(searchPath, []byte(x.SearchString), 0644); err != nil {
		return err
	}
	x.SearchString = ``
	mb, err := json.Marshal(x, jsoncompat.Options, jsontext.WithIndentPrefix(""), jsontext.WithIndent("	"))
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, mb, 0644)
}

func readScheduledSearch(dir, id string) (x kits.PackedScheduledSearch, err error) {
	p := filepath.Join(dir, "scheduled")
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	searchPath := filepath.Join(p, fmt.Sprintf("%v.search", id))
	// Read the metadata file first
	var bts []byte
	bts, err = os.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x, jsoncompat.Options); err != nil {
		return
	}
	bts, err = os.ReadFile(searchPath)
	if err != nil {
		return
	}
	x.SearchString = string(bts)
	return
}

/**************************************************************************
 * Scheduled Script
 **************************************************************************/

func writeScheduledScript(dir string, id string, x kits.PackedScheduledScript) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "scheduled")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop files
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	scriptPath := filepath.Join(p, fmt.Sprintf("%v.script", id))
	if err := os.WriteFile(scriptPath, []byte(x.Script), 0644); err != nil {
		return err
	}
	x.Script = ``
	mb, err := json.Marshal(x, jsoncompat.Options, jsontext.WithIndentPrefix(""), jsontext.WithIndent("	"))
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, mb, 0644)
}

func readScheduledScript(dir, id string) (x kits.PackedScheduledScript, err error) {
	p := filepath.Join(dir, "scheduled")
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	scriptPath := filepath.Join(p, fmt.Sprintf("%v.script", id))
	// Read the metadata file first
	var bts []byte
	bts, err = os.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x, jsoncompat.Options); err != nil {
		return
	}
	// Now read script file
	bts, err = os.ReadFile(scriptPath)
	if err != nil {
		return
	}
	x.Script = string(bts)
	return
}

/**************************************************************************
 * Flow
 **************************************************************************/

func writeFlow(dir string, id string, x kits.PackedFlow) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "scheduled")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Now drop files
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	flowPath := filepath.Join(p, fmt.Sprintf("%v.flow", id))
	if err := os.WriteFile(flowPath, []byte(x.Flow), 0644); err != nil {
		return err
	}
	x.Flow = ``
	mb, err := json.Marshal(x, jsoncompat.Options, jsontext.WithIndentPrefix(""), jsontext.WithIndent("	"))
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, mb, 0644)
}

func readFlow(dir, id string) (x kits.PackedFlow, err error) {
	p := filepath.Join(dir, "scheduled")
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	flowPath := filepath.Join(p, fmt.Sprintf("%v.flow", id))
	// Read the metadata file first
	var bts []byte
	bts, err = os.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x, jsoncompat.Options); err != nil {
		return
	}
	bts, err = os.ReadFile(flowPath)
	if err != nil {
		return
	}
	x.Flow = string(bts)
	return
}

/**************************************************************************
 * Dashboard
 **************************************************************************/

func writeDashboard(dir string, id string, x kits.PackedDashboard) error {
	// Make sure the parent exists
	p := filepath.Join(dir, "dashboard")
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Just one file for now
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	mb, err := json.Marshal(x, jsoncompat.Options, jsontext.WithIndentPrefix(""), jsontext.WithIndent("	"))
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, mb, 0644)
}

func readDashboard(dir, id string) (x kits.PackedDashboard, err error) {
	p := filepath.Join(dir, "dashboard")
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", id))
	// Read the metadata file
	var bts []byte
	bts, err = os.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, &x, jsoncompat.Options); err != nil {
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
	return os.WriteFile(lPath, x, 0644)
}

func readLicense(dir, name string) (x []byte, err error) {
	p := filepath.Join(dir, "license")
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", name))

	x, err = os.ReadFile(metaPath)
	return
}

/**************************************************************************
 * Generic
 **************************************************************************/

func genericWrite(dir string, itm types.KitItem, x interface{}) error {
	// Make sure the parent exists
	p := filepath.Join(dir, string(itm.Type))
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}

	// Just drop it all in a single file
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", itm.ID))
	mb, err := json.Marshal(x, jsoncompat.Options, jsontext.WithIndentPrefix(""), jsontext.WithIndent("	"))
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, mb, 0644)
}

func genericRead(dir string, itm types.KitItem, obj interface{}) (err error) {
	p := filepath.Join(dir, string(itm.Type))
	metaPath := filepath.Join(p, fmt.Sprintf("%v.meta", itm.ID))
	// Read the metadata file
	var bts []byte
	bts, err = os.ReadFile(metaPath)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bts, obj, jsoncompat.Options); err != nil {
		return
	}
	return
}
