/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/gravwell/gravwell/v3/client/types/kits"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

var (
	fID     = flag.String("id", "", "Kit ID")
	fName   = flag.String("name", "", "Kit/item name")
	fDesc   = flag.String("desc", "", "Kit/item description")
	fMinVer = flag.String("minver", "", "Minimum version")
	fMaxVer = flag.String("maxver", "", "Maximum version")

	fDefaultValue = flag.String("default-value", "", "Default value")
	fMacroType    = flag.String("macro-type", "", "Config macro type ('tag' or 'other')")
)

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		log.Printf("Must specify a command.")
		return
	}

	switch args[0] {
	case "unpack":
		// Unpack the given kit into the current directory
		unpackKit(args[1:])
	case "pack":
		// Pack the current directory into a kit
		packKit(args[1:])
	case "info":
		// Information about the kit in the current directory
		log.Fatalf("%v not implemented", args[0])
	case "init":
		// Start a new kit from scratch in the current directory
		initKit(args[1:])
	case "scan":
		// Attempt to reconcile what's on-disk vs. what's in the manifest
		log.Fatalf("%v not implemented", args[0])
	case "set":
		// Set various fields of the kit
		log.Fatalf("%v not implemented", args[0])
	case "dep":
		// Manage dependencies
		log.Fatalf("%v not implemented", args[0])
	case "configmacro":
		// Manage config macros
		configMacro(args[1:])
	default:
		log.Fatalf("Invalid command %v.", args[0])
	}
}

func configMacro(args []string) {
	mf, err := readManifest()
	if err != nil {
		log.Fatal(err)
	}
	if len(args) == 0 {
		fmt.Printf("Usage:\n")
		fmt.Printf("	kitctl configmacro list		# show existing macros\n")
		fmt.Printf("	kitctl configmacro show		# show detailed info about a particular macro\n")
		fmt.Printf("	kitctl configmacro add		# add new macro\n")
		fmt.Printf("	kitctl configmacro del		# delete macro\n")
		return
	}
	switch args[0] {
	case "list":
		for _, m := range mf.ConfigMacros {
			fmt.Println(m.MacroName)
		}
	case "show":
		if *fName == `` {
			log.Fatalf("Must specify macro to show with -name flag")
		}
		for _, m := range mf.ConfigMacros {
			if m.MacroName == *fName {
				fmt.Printf("Name: %v\n", m.MacroName)
				fmt.Printf("Description: %v\n", m.Description)
				fmt.Printf("Default value: %v\n", m.DefaultValue)
				fmt.Printf("Type: %v\n", m.Type)
				break
			}
		}
	case "add":
		// Make sure all the required flags are set
		var fail bool
		if *fName == `` {
			fail = true
			log.Printf("Must set macro name with -name flag")
		}
		if *fDesc == `` {
			fail = true
			log.Printf("Must set macro description with -desc flag")
		}
		if *fDefaultValue == `` {
			fail = true
			log.Printf("Must set macro default value with -default-value flag")
		}
		var mType string
		if *fMacroType == `` {
			fail = true
			log.Printf("Must set macro type with -macro-type flag")
		} else {
			mType = strings.ToUpper(*fMacroType)
			if mType != "TAG" && mType != "OTHER" {
				log.Fatalf("Macro type must be either 'tag' or 'other'")
			}
		}
		if fail {
			log.Fatalf("Aborting configmacro add")
		}
		// Make sure the macro name is ok
		if err := types.CheckMacroName(*fName); err != nil {
			log.Fatalf("Macro name contains illegal character. Allowed characters: %v", types.AllowedMacroChars)
		}
		// Now walk all the existing macros and make sure it doesn't conflict
		// Start with config macros
		for _, m := range mf.ConfigMacros {
			if m.MacroName == *fName {
				log.Fatalf("New config macro conflicts with existing config macro: %+v", m)
			}
		}
		// And then check regular macros
		for _, m := range mf.Items {
			if m.Type == kits.Macro && m.Name == *fName {
				log.Fatalf("New config macro conflicts with existing regular macro: %+v", m)
			}
		}
		// Create the config macro
		cm := types.KitConfigMacro{
			MacroName:    *fName,
			Description:  *fDesc,
			DefaultValue: *fDefaultValue,
			Type:         mType,
		}
		// Insert it into the manifest
		mf.ConfigMacros = append(mf.ConfigMacros, cm)
		// Write out the manifest
		writeManifest(mf)
	case "del":
		if *fName == `` {
			log.Fatalf("Must specify macro to delete with -name flag")
		}
		for i, m := range mf.ConfigMacros {
			if m.MacroName == *fName {
				mf.ConfigMacros = append(mf.ConfigMacros[:i], mf.ConfigMacros[i+1:]...)
				break
			}
		}
		writeManifest(mf)
	default:
		log.Fatalf("Invalid configmacro command %v", args[0])
	}
}

func initKit(args []string) {
	var err error
	// Make sure there's not already a kit here
	if _, err = os.Stat("MANIFEST"); !os.IsNotExist(err) {
		log.Fatalf("MANIFEST file already exists, aborting")
	}

	// Parse and validate args that need parsing
	var minver, maxver types.CanonicalVersion
	if *fMinVer != `` {
		minver, err = types.ParseCanonicalVersion(*fMinVer)
		if err != nil {
			log.Fatalf("Could not parse minver: %v", err)
		}
	}
	if *fMaxVer != `` {
		maxver, err = types.ParseCanonicalVersion(*fMaxVer)
		if err != nil {
			log.Fatalf("Could not parse maxver: %v", err)
		}
	}
	// Make sure max > min, if max is set. Yes, this works, the Compare function is confusing.
	if maxver.Enabled() && maxver.Compare(minver) > 0 {
		log.Fatalf("Max version must be either zero or greater than minver.")
	}

	// Create a Manifest structure, populating with command-line flags if set
	mf := kits.Manifest{
		ID:         *fID,
		Name:       *fName,
		Desc:       *fDesc,
		Version:    1,
		MinVersion: minver,
		MaxVersion: maxver,
	}

	// Write manifest to disk
	mb, err := json.MarshalIndent(mf, "", "	")
	if err != nil {
		log.Fatalf("Failed to re-marshal MANIFEST: %v", err)
	}
	if err := ioutil.WriteFile("MANIFEST", mb, 0644); err != nil {
		log.Fatalf("Failed to write-out MANIFEST file: %v", err)
	}
}

func packKit(args []string) {
	// Figure out where we are
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Couldn't figure out working directory: %v", err)
	}
	// Check args
	if len(args) != 1 {
		fmt.Printf("Usage: kitctl pack <outfile>\n")
		return
	}

	mf, err := readManifest()
	if err != nil {
		log.Fatal(err)
	}

	// Prepare the BuilderConfig
	bc := kits.BuilderConfig{
		Version:      mf.Version,
		Name:         mf.Name,
		Description:  mf.Desc,
		ID:           mf.ID,
		MinVersion:   mf.MinVersion,
		MaxVersion:   mf.MaxVersion,
		Dependencies: mf.Dependencies,
		ConfigMacros: mf.ConfigMacros,
	}
	bc.Version++

	// Get builder
	bldr, err := kits.NewBuilderFile(bc, args[0])
	if err != nil {
		log.Fatalf("Could not get builder: %v", err)
	}

	marshallAdd := func(itm kits.Item, obj interface{}) error {
		bts, err := json.Marshal(obj)
		if err != nil {
			return fmt.Errorf("Could not marshal %v %v: %v", itm.Type.String(), itm.Name, err)
		}
		if err := bldr.Add(itm.Name, itm.Type, bts); err != nil {
			return fmt.Errorf("Couldn't add %v %v: %v", itm.Type.String(), itm.Name, err)
		}
		return nil
	}

	// Walk each kit item in the manifest and add it
	for _, itm := range mf.Items {
		switch itm.Type {
		// Some types have special "packed" versions
		case kits.Resource:
			pr, err := readResource(wd, itm.Name)
			if err != nil {
				log.Fatalf("Could not read resource %v: %v", itm.Name, err)
			}
			if err := marshallAdd(itm, pr); err != nil {
				log.Fatal(err)
			}
		case kits.Macro:
			pm, err := readMacro(wd, itm.Name)
			if err != nil {
				log.Fatalf("Could not read macro %v: %v", itm.Name, err)
			}
			if err := marshallAdd(itm, pm); err != nil {
				log.Fatal(err)
			}
		case kits.ScheduledSearch:
			x, err := readScheduledSearch(wd, itm.Name)
			if err != nil {
				log.Fatalf("Could not read scheduled search %v: %v", itm.Name, err)
			}
			if err := marshallAdd(itm, x); err != nil {
				log.Fatal(err)
			}
		case kits.Dashboard:
			x, err := readDashboard(wd, itm.Name)
			if err != nil {
				log.Fatalf("Could not read dashboard %v: %v", itm.Name, err)
			}
			if err := marshallAdd(itm, x); err != nil {
				log.Fatal(err)
			}
		case kits.Template:
			var x types.PackedUserTemplate
			if x, err = readTemplate(wd, itm.Name); err != nil {
				log.Fatalf("Could not read %v %v: %v", itm.Type.String(), itm.Name, err)
			}
			if err := marshallAdd(itm, x); err != nil {
				log.Fatal(err)
			}
		case kits.Pivot:
			var x types.PackedPivot
			if err := genericRead(wd, itm, &x); err != nil {
				log.Fatalf("Could not read %v %v: %v", itm.Type.String(), itm.Name, err)
			}
			if err := marshallAdd(itm, x); err != nil {
				log.Fatal(err)
			}
		// Other types just ship as-is
		case kits.Extractor:
			var x types.AXDefinition
			if x, err = readExtractor(wd, itm.Name); err != nil {
				log.Fatalf("Could not read %v %v: %v", itm.Type.String(), itm.Name, err)
			}
			if err := marshallAdd(itm, x); err != nil {
				log.Fatal(err)
			}
		case kits.File:
			var x types.UserFile
			if x, err = readUserFile(wd, itm.Name); err != nil {
				log.Fatalf("Could not read %v %v: %v", itm.Type.String(), itm.Name, err)
			}
			if err := marshallAdd(itm, x); err != nil {
				log.Fatal(err)
			}
		case kits.SearchLibrary:
			var x types.WireSearchLibrary
			if x, err = readSearchLibrary(wd, itm.Name); err != nil {
				log.Fatalf("Could not read %v %v: %v", itm.Type.String(), itm.Name, err)
			}
			if err := marshallAdd(itm, x); err != nil {
				log.Fatal(err)
			}
		case kits.Playbook:
			var x types.Playbook
			if x, err = readPlaybook(wd, itm.Name); err != nil {
				log.Fatalf("Could not read %v %v: %v", itm.Type.String(), itm.Name, err)
			}
			if err := marshallAdd(itm, x); err != nil {
				log.Fatal(err)
			}
		case kits.License:
			x, err := readLicense(wd, itm.Name)
			if err != nil {
				log.Fatalf("Could not read license %v: %v", itm.Name, err)
			}
			if err := bldr.Add(itm.Name, itm.Type, x); err != nil {
				log.Fatalf("Couldn't add %v %v: %v", itm.Type.String(), itm.Name, err)
			}
		default:
			log.Fatalf("Error parsing item %v, unknown item type %v", itm.Name, itm.Type)
		}
	}

	// The last step: set the icon, banner, and cover
	if mf.Icon != "" {
		if err := bldr.SetIcon(mf.Icon); err != nil {
			log.Fatalf("Could not set icon: %v", err)
		}
	}
	if mf.Cover != "" {
		if err := bldr.SetCover(mf.Cover); err != nil {
			log.Fatalf("Could not set cover: %v", err)
		}
	}
	if mf.Banner != "" {
		if err := bldr.SetBanner(mf.Banner); err != nil {
			log.Fatalf("Could not set banner: %v", err)
		}
	}

	if err = bldr.WriteManifest(nil); err != nil {
		log.Fatalf("Could not write manifest: %v", err)
	} else if err = bldr.Close(); err != nil {
		log.Fatalf("Could not close builder: %v", err)
	}
}

func unpackKit(args []string) {
	// Figure out where we are
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Couldn't figure out working directory: %v", err)
	}

	// Open the file
	if len(args) != 1 {
		fmt.Printf("Usage: kitctl unpack <kitfile>\n")
		return
	}
	fi, err := utils.OpenFileReader(args[0])
	if err != nil {
		log.Fatalf("Could not open file %v: %v", args[0], err)
	}

	// Get reader
	rdr, err := kits.NewReader(fi, nil)
	if err != nil {
		log.Fatalf("Could not get reader for kit file: %v", err)
	}
	if err := rdr.Verify(); err != nil {
		log.Fatalf("Could not verify kit: %v", err)
	}

	// Copy out the MANIFEST file
	mf, err := rdr.Manifest()
	if err != nil {
		log.Fatalf("Failed to read manifest: %v", err)
	}

	if err := writeManifest(mf); err != nil {
		log.Fatal(err)
	}

	// Walk each kit item
	err = rdr.Process(func(name string, tp kits.ItemType, hash [sha256.Size]byte, rdr io.Reader) error {
		// For each item:
		// Verify the hash of the file
		// Unmarshal the item
		// Write it out into split content/metadata files.
		switch tp {
		// These types have special "packed" versions
		case kits.Resource:
			var pr kits.PackedResource
			if err = json.NewDecoder(rdr).Decode(&pr); err != nil {
				return fmt.Errorf("Failed to decode resource %v: %v", name, err)
			}
			pr.ResourceName = name
			if err = pr.Validate(); err != nil {
				return fmt.Errorf("Failed to validate resource %v: %v", name, err)
			}
			// We write out the resource into two separate files
			if err := writeResource(wd, pr); err != nil {
				return fmt.Errorf("Failed to write out resource %v: %v", name, err)
			}
		case kits.Macro:
			var pm kits.PackedMacro
			if err = json.NewDecoder(rdr).Decode(&pm); err != nil {
				return fmt.Errorf("Failed to decode macro %v: %v", name, err)
			}
			if err = pm.Validate(); err != nil {
				return fmt.Errorf("Failed to validate macro %v: %v", name, err)
			}
			if err := writeMacro(wd, pm); err != nil {
				return fmt.Errorf("Failed to write out macro %v: %v", name, err)
			}
		case kits.ScheduledSearch:
			var p kits.PackedScheduledSearch
			if err = json.NewDecoder(rdr).Decode(&p); err != nil {
				return fmt.Errorf("Failed to decode scheduled search %v: %v", name, err)
			}
			if err = p.Validate(); err != nil {
				return fmt.Errorf("Failed to validate scheduled search %v: %v", name, err)
			}
			if err := writeScheduledSearch(wd, p); err != nil {
				return fmt.Errorf("Failed to write out scheduled search %v: %v", name, err)
			}
		case kits.Dashboard:
			var p kits.PackedDashboard
			if err = json.NewDecoder(rdr).Decode(&p); err != nil {
				return fmt.Errorf("Failed to decode dashboard %v: %v", name, err)
			}
			if err = p.Validate(); err != nil {
				return fmt.Errorf("Failed to validate dashboard %v: %v", name, err)
			}
			if err := writeDashboard(wd, name, p); err != nil {
				return fmt.Errorf("Failed to write out dashboard %v: %v", name, err)
			}
		case kits.Template:
			var p types.PackedUserTemplate
			if err = json.NewDecoder(rdr).Decode(&p); err != nil {
				return fmt.Errorf("Failed to decode %v %v: %v", tp.String(), name, err)
			}
			if err := writeTemplate(wd, name, p); err != nil {
				return fmt.Errorf("Failed to write out %v %v: %v", tp.String(), name, err)
			}
		case kits.Pivot:
			var p types.PackedPivot
			if err = json.NewDecoder(rdr).Decode(&p); err != nil {
				return fmt.Errorf("Failed to decode %v %v: %v", tp.String(), name, err)
			}
			if err := genericWrite(wd, tp, name, p); err != nil {
				return fmt.Errorf("Failed to write out %v %v: %v", tp.String(), name, err)
			}
		// Other types just ship as-is
		case kits.Extractor:
			var p types.AXDefinition
			if err = json.NewDecoder(rdr).Decode(&p); err != nil {
				return fmt.Errorf("Failed to decode extractor %v: %v", name, err)
			}
			if err = p.Validate(); err != nil {
				return fmt.Errorf("Failed to validate extractor %v: %v", name, err)
			}
			if err := writeExtractor(wd, name, p); err != nil {
				return fmt.Errorf("Failed to write out %v %v: %v", tp.String(), name, err)
			}
		case kits.File:
			var p types.UserFile
			if err = json.NewDecoder(rdr).Decode(&p); err != nil {
				return fmt.Errorf("Failed to decode %v %v: %v", tp.String(), name, err)
			}
			if err := writeUserFile(wd, name, p); err != nil {
				return fmt.Errorf("Failed to write out %v %v: %v", tp.String(), name, err)
			}
		case kits.SearchLibrary:
			var p types.WireSearchLibrary
			if err = json.NewDecoder(rdr).Decode(&p); err != nil {
				return fmt.Errorf("Failed to decode %v %v: %v", tp.String(), name, err)
			}
			if err := writeSearchLibrary(wd, name, p); err != nil {
				return fmt.Errorf("Failed to write out %v %v: %v", tp.String(), name, err)
			}
		case kits.Playbook:
			var p types.Playbook
			if err = json.NewDecoder(rdr).Decode(&p); err != nil {
				return fmt.Errorf("Failed to decode %v %v: %v", tp.String(), name, err)
			}
			if err := writePlaybook(wd, name, p); err != nil {
				return fmt.Errorf("Failed to write out %v %v: %v", tp.String(), name, err)
			}
		case kits.License:
			var p []byte
			if p, err = ioutil.ReadAll(rdr); err != nil {
				return fmt.Errorf("Failed to decode %v %v: %v", tp.String(), name, err)
			}
			if err := writeLicense(wd, name, p); err != nil {
				return fmt.Errorf("Failed to write out %v %v: %v", tp.String(), name, err)
			}
		default:
			return fmt.Errorf("Error parsing item %v, unknown item type %v", name, tp)
		}
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}
}
