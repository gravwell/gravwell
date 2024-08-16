/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/gravwell/gravwell/v4/client/types/kits"
)

func readManifest() (kits.Manifest, error) {
	// Get the manifest file
	var mf kits.Manifest
	mb, err := ioutil.ReadFile("MANIFEST")
	if err != nil {
		return mf, fmt.Errorf("Couldn't read MANIFEST: %v", err)
	}
	if err := json.Unmarshal(mb, &mf); err != nil {
		return mf, fmt.Errorf("Couldn't parse MANIFEST: %v", err)
	}
	return mf, nil
}

func writeManifest(mf kits.Manifest) error {
	// And write the MANIFEST back out onto disk
	mb, err := json.MarshalIndent(mf, "", "	")
	if err != nil {
		return fmt.Errorf("Failed to re-marshal MANIFEST: %v", err)
	}
	if err := ioutil.WriteFile("MANIFEST", mb, 0644); err != nil {
		return fmt.Errorf("Failed to write-out MANIFEST file: %v", err)
	}
	return nil
}
