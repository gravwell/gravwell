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

	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/gravwell/gravwell/v3/client/types/kits"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

var (
	fID     = flag.String("id", "", "Kit ID")
	fName   = flag.String("name", "", "Kit name")
	fDesc   = flag.String("desc", "", "Kit description")
	fMinVer = flag.String("minver", "", "Minimum version")
	fMaxVer = flag.String("maxver", "", "Maximum version")
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
	default:
		log.Fatalf("Invalid command %v.", args[0])
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

	// Get the manifest file
	mb, err := ioutil.ReadFile("MANIFEST")
	if err != nil {
		log.Fatalf("Couldn't read MANIFEST: %v", err)
	}
	var mf kits.Manifest
	if err := json.Unmarshal(mb, &mf); err != nil {
		log.Fatalf("Couldn't parse MANIFEST: %v", err)
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

	// And write the MANIFEST back out onto disk
	mb, err := json.MarshalIndent(mf, "", "	")
	if err != nil {
		log.Fatalf("Failed to re-marshal MANIFEST: %v", err)
	}
	if err := ioutil.WriteFile("MANIFEST", mb, 0644); err != nil {
		log.Fatalf("Failed to write-out MANIFEST file: %v", err)
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
