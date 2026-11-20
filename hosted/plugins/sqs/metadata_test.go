/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package sqs

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestIconIsEmbeddedVerbatim checks the icon travels with the binary and that it is the
// file on disk, unaltered.  It is third party brand artwork: the licence record beside it
// names a checksum, so anything that quietly rewrote it would make that record false.
func TestIconIsEmbeddedVerbatim(t *testing.T) {
	onDisk, err := os.ReadFile(`sqs.svg`)
	if err != nil {
		t.Fatal(err)
	}
	if Icon != string(onDisk) {
		t.Fatalf("Icon is %d bytes, sqs.svg is %d, they must be identical", len(Icon), len(onDisk))
	}
	if !strings.Contains(Icon, `<svg`) || !strings.Contains(Icon, `</svg>`) {
		t.Error(`Icon is not an SVG`)
	}
}

// TestRunnerMetadataIsComplete covers what reaches a webserver when this plugin registers.
func TestRunnerMetadataIsComplete(t *testing.T) {
	md := (&Config{}).RunnerMetadata()
	if md == nil {
		t.Fatal(`the plugin describes nothing, so it registers with no icon, version or docs`)
	}
	if md.Icon != Icon {
		t.Error(`the icon is not carried in the metadata`)
	}
	if !md.Version.Enabled() || md.Version.String() != Version {
		t.Errorf("version = %q, want %q", md.Version, Version)
	}
	if len(md.Documentation) == 0 {
		t.Error(`no documentation links`)
	}
}

// TestIconLicenseMatchesTheIcon keeps the provenance record honest.
//
// It records a checksum, and a checksum that has drifted from the file it describes is
// worse than none: it reads as a verified fact and is not one. The file is third party
// brand artwork, so this is the only record of where it came from.
func TestIconLicenseMatchesTheIcon(t *testing.T) {
	doc, err := os.ReadFile(`ICON_LICENSE.md`)
	if err != nil {
		t.Fatal(err)
	}
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(Icon)))
	if !strings.Contains(string(doc), sum) {
		t.Errorf("ICON_LICENSE.md does not record the icon's checksum %s", sum)
	}
	if !strings.Contains(string(doc), fmt.Sprintf("%d bytes", len(Icon))) {
		t.Errorf("ICON_LICENSE.md does not record the icon's size of %d bytes", len(Icon))
	}
	// the trademark notice is the part that is not optional: CC0 covers the file as a
	// creative work and cannot grant rights in the mark it depicts
	for _, must := range []string{`CC0`, `trademark`, `Amazon`} {
		if !strings.Contains(string(doc), must) {
			t.Errorf("ICON_LICENSE.md no longer mentions %q", must)
		}
	}
}

// TestIconCarriesNoProvenanceMetadata is the other half of that record.  The checksum is
// of the artwork alone, which is only true while the file has nothing else in it: an
// exporter that put a manifest back would change what is being attested to.
func TestIconCarriesNoProvenanceMetadata(t *testing.T) {
	low := strings.ToLower(Icon)
	for _, never := range []string{`<metadata`, `<!--`, `<?xml`, `c2pa`, `jumb`, `<title`, `<desc`,
		`inkscape`, `sketch`, `illustrator`, `xmlns:dc`, `xmlns:rdf`} {
		if strings.Contains(low, never) {
			t.Errorf("the icon carries %q, which is not artwork", never)
		}
	}
	// pure ASCII: a stray byte is either mojibake or something hiding
	for i := 0; i < len(Icon); i++ {
		if Icon[i] < 0x20 || Icon[i] > 0x7e {
			t.Errorf("byte %d is 0x%02x, outside printable ASCII", i, Icon[i])
			break
		}
	}
}
