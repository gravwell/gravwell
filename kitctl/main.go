/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
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
	"log"
	"os"
	"strings"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/client/types/kits"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
)

var (
	fID      = flag.String("id", "", "Kit ID")
	fName    = flag.String("name", "", "Kit/item name")
	fDesc    = flag.String("desc", "", "Kit/item description")
	fVersion = flag.Uint("version", 0, "Kit version")
	fMinVer  = flag.String("minver", "", "Minimum version")
	fMaxVer  = flag.String("maxver", "", "Maximum version")

	fDefaultValue = flag.String("default-value", "", "Default value")
	fMacroType    = flag.String("macro-type", "", "Config macro type ('tag' or 'other')")
)

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		log.Printf("Must specify a command.")
		help([]string{})
		return
	}

	switch args[0] {
	case "help":
		help(args[1:])
	case "unpack":
		// Unpack the given kit into the current directory
		unpackKit(args[1:])
	case "pack":
		// Pack the current directory into a kit
		packKit(args[1:])
	case "import":
		// Merge in the contents of another kit file
		importKit(args[1:])
	case "info":
		// Information about the kit in the current directory
		kitInfo(args[1:])
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
		dep(args[1:])
	case "configmacro":
		// Manage config macros
		configMacro(args[1:])
	default:
		log.Fatalf("Invalid command %v. Try kitctl help", args[0])
	}
}

func help(args []string) {
	fmt.Printf("Usage: kitctl [flags] <cmd> [arguments]\n\n")
	fmt.Printf("kitctl provides tools for working with a Gravwell kit managed inside a git repository. It unpacks a kit archive file into discrete files which can be more easily modified. Once modifications are done, it can re-pack the contents into an archive file again.\n\n")
	fmt.Printf("Commands:\n")
	fmt.Println("	unpack <input file>: unpack a kit into the current directory")
	fmt.Println("	pack <output file>: pack the current directory into a kit file")
	fmt.Println("	import <input file>: include the contents of another kit into the already-unpacked kit in the current directory")
	fmt.Println("	info: prints information about the kit in the current directory")
	fmt.Println("	init: starts a new kit from scratch in the current directory")
	fmt.Println("	dep list: list the current kit's dependencies")
	fmt.Println("	dep add: add another dependency to the current kit")
	fmt.Println("	dep del: delete a dependency from the current kit")
	fmt.Println("	configmacro list: list the kit's config macros")
	fmt.Println("	configmacro show: show info about a particular config macro")
	fmt.Println("	configmacro add: add a new config macro to the kit")
	fmt.Println("	configmacro del: delete a config macro from the kit")
	fmt.Println("")
	fmt.Println("Flags:")
	flag.PrintDefaults()
}

// the "info" command just prints out some basic details about the kit for now.
func kitInfo(args []string) {
	mf, err := readManifest()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("•Kit ID: %v\n", mf.ID)
	fmt.Printf("•Name: %v\n", mf.Name)
	fmt.Printf("•Description: %v\n", mf.Desc)
	fmt.Printf("•Version: %v\n", mf.Version)
	fmt.Printf("•Minimum Gravwell version required: %v\n", mf.MinVersion)
	fmt.Printf("•Maximum Gravwell version allowed: %v\n", mf.MaxVersion)
	fmt.Printf("•Dependencies:\n")
	if len(mf.Dependencies) > 0 {
		for _, d := range mf.Dependencies {
			fmt.Printf("	%v >= %v\n", d.ID, d.MinVersion)
		}
	} else {
		fmt.Printf("	none\n")
	}
	fmt.Printf("•Items:\n")
	if len(mf.Items) > 0 {
		for _, d := range mf.Items {
			fmt.Printf("	%v			%v\n", d.Type, d.Name)
		}
	} else {
		fmt.Printf("	none\n")
	}
}

func dep(args []string) {
	mf, err := readManifest()
	if err != nil {
		log.Fatal(err)
	}
	if len(args) == 0 {
		fmt.Printf("Usage:\n")
		fmt.Printf("	kitctl dep list		# show existing dependencies\n")
		fmt.Printf("	kitctl dep add		# add new dependency\n")
		fmt.Printf("	kitctl dep del		# delete dependency\n")
		return
	}
	switch args[0] {
	case "list":
		for _, m := range mf.Dependencies {
			fmt.Printf("%v >= %v\n", m.ID, m.MinVersion)
		}
	case "add":
		// Make sure all the required flags are set
		var fail bool
		if *fID == `` {
			fail = true
			log.Printf("Must set dependency ID with -id flag")
		}
		if *fVersion == 0 {
			fail = true
			log.Printf("Must set minimum dependency version with -version flag")
		}
		if fail {
			log.Fatalf("Aborting dep add")
		}
		// Now walk all the existing dependencies and make sure it doesn't conflict
		for _, m := range mf.Dependencies {
			if m.ID == *fID {
				log.Fatalf("New dependency conflicts with existing: %+v", m)
			}
		}
		// Create the dep
		d := types.KitDependency{
			ID:         *fID,
			MinVersion: *fVersion,
		}
		// Insert it into the manifest
		mf.Dependencies = append(mf.Dependencies, d)
		// Write out the manifest
		writeManifest(mf)
	case "del":
		if *fID == `` {
			log.Fatalf("Must specify dependency to delete with -id flag")
		}
		for i, m := range mf.Dependencies {
			if m.ID == *fID {
				mf.Dependencies = append(mf.Dependencies[:i], mf.Dependencies[i+1:]...)
				break
			}
		}
		writeManifest(mf)
	default:
		log.Fatalf("Invalid dep command %v", args[0])
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
	version := uint(1)
	if *fVersion > 0 {
		version = *fVersion
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
		Version:    version,
		MinVersion: minver,
		MaxVersion: maxver,
	}

	// Write manifest to disk
	mb, err := json.MarshalIndent(mf, "", "	")
	if err != nil {
		log.Fatalf("Failed to re-marshal MANIFEST: %v", err)
	}
	if err := os.WriteFile("MANIFEST", mb, 0644); err != nil {
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
	// Note that we no longer automatically bump the version; do that yourself.
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
		case kits.Alert:
			var x types.AlertDefinition
			if err = genericRead(wd, itm, &x); err != nil {
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

func importKit(args []string) {
	// Figure out where we are
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Couldn't figure out working directory: %v", err)
	}

	// Check args
	if len(args) != 1 {
		fmt.Printf("Usage: kitctl import <kitfile>\n")
		return
	}

	// Get the original manifest
	mf, err := readManifest()
	if err != nil {
		log.Fatal(err)
	}

	// Open the new file
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
	newmf, err := rdr.Manifest()
	if err != nil {
		log.Fatalf("Failed to read manifest: %v", err)
	}

	// Read out the files from the incoming kit and put them on disk
	// We do this first so the files are here, even if the manifest merging fails
	if err := unpackKitItems(wd, rdr); err != nil {
		log.Fatal(err)
	}

	// Merge the incoming stuff from the new manifest
	// What do we do if there's a conflict?
	// Well, the most likely use-case is that it happens because you're
	// updating some items in a kit, so we're just gonna overwrite.
	// But we'll notify, too.
	// First, merge Items
itemLoop:
	for i := range newmf.Items {
		for j := range mf.Items {
			if newmf.Items[i].Name == mf.Items[j].Name && newmf.Items[i].Type == mf.Items[j].Type {
				log.Printf("Replacing existing item %v", newmf.Items[i].Name)
				mf.Items[j] = newmf.Items[i]
				continue itemLoop
			}
		}
		// No conflict, just append
		log.Printf("Importing new kit item %v", newmf.Items[i].Name)
		mf.Items = append(mf.Items, newmf.Items[i])
	}

	// Next, merge ConfigMacros
macroLoop:
	for i := range newmf.ConfigMacros {
		for j := range mf.ConfigMacros {
			if newmf.ConfigMacros[i].MacroName == mf.ConfigMacros[j].MacroName {
				log.Printf("Replacing existing config macro %v", newmf.ConfigMacros[i].MacroName)
				mf.ConfigMacros[j] = newmf.ConfigMacros[i]
				continue macroLoop
			}
		}
		// No conflict, just append
		log.Printf("Importing new config macro %v", newmf.ConfigMacros[i].MacroName)
		mf.ConfigMacros = append(mf.ConfigMacros, newmf.ConfigMacros[i])
	}

	// Write out the new manifest
	if err := writeManifest(mf); err != nil {
		log.Fatal(err)
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

	if err := unpackKitItems(wd, rdr); err != nil {
		log.Fatal(err)
	}
}

func unpackKitItems(wd string, rdr *kits.Reader) error {
	// Walk each kit item
	return rdr.Process(func(name string, tp kits.ItemType, hash [sha256.Size]byte, rdr io.Reader) error {
		var err error
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
			if err := writeScheduledSearch(wd, name, p); err != nil {
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
		case kits.Alert:
			var p types.AlertDefinition
			if err = json.NewDecoder(rdr).Decode(&p); err != nil {
				return fmt.Errorf("Failed to decode %v %v: %v", tp.String(), name, err)
			}
			if err := genericWrite(wd, tp, name, p); err != nil {
				return fmt.Errorf("Failed to write out %v %v: %v", tp.String(), name, err)
			}
		case kits.License:
			var p []byte
			if p, err = io.ReadAll(rdr); err != nil {
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
}
