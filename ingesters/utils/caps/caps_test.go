/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package caps

import (
	"os"
	"testing"
)

func TestCaps(t *testing.T) {
	//because this might be run in a docker container with root, check that too
	if uid := os.Getuid(); uid == 0 {
		//running as root
		for i := minCap; i <= maxCap; i++ {
			if !Has(i) {
				t.Fatalf("root user cap %v missing", i)
			}
		}
		if caps, err := GetCaps(); err != nil {
			t.Fatal(err)
		} else if caps != All {
			t.Fatal("root user does not have all caps")
		}
	} else if uid < 0 {
		//on an unsupported platform, just bail
		return
	} else {
		if caps, err := GetCaps(); err != nil {
			t.Fatal(err)
		} else if caps != 0 {
			t.Fatal("non-root user has caps")
		}
		for i := minCap; i <= maxCap; i++ {
			if Has(i) {
				t.Fatalf("non-root user has cap %v", i)
			}
		}
	}
}
