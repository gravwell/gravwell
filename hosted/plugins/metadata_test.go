/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package plugins

import (
	"net/url"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
)

// TestEveryPluginDescribesItself is where a malformed version is caught.
//
// dynamic.Version deliberately swallows a parse failure, because an ingester is not worth
// taking down over a typo in a field that exists to be printed next to a name.  That
// choice is only defensible if something else notices, so this is that something.
func TestEveryPluginDescribesItself(t *testing.T) {
	kinds, err := Kinds()
	if err != nil {
		t.Fatal(err)
	}
	if len(kinds) == 0 {
		t.Fatal(`no plugins enumerated`)
	}
	for _, pk := range kinds {
		t.Run(pk.Kind, func(t *testing.T) {
			rd, err := dynamic.MapRunnerDefinition(pk.Kind, pk.Kind, pk.Config)
			if err != nil {
				t.Fatal(err)
			}
			md := rd.Metadata
			if md == nil {
				t.Fatal(`plugin does not implement dynamic.MetadataProvider, so it registers with no icon, version or documentation`)
			}
			if !md.Version.Enabled() {
				t.Errorf(`version did not parse to anything, it must be canonical major.minor.point`)
			}
			if len(md.Documentation) == 0 {
				t.Error(`plugin offers no documentation links`)
			}
			for _, dl := range md.Documentation {
				if dl.Name == `` {
					t.Errorf("documentation link %q has no name to render", dl.Link)
				}
				u, err := url.Parse(dl.Link)
				if err != nil {
					t.Errorf("documentation link %q does not parse: %v", dl.Link, err)
					continue
				}
				// these land in an href, so anything but plain http(s) is a scheme that
				// has no business being clickable in an operator's browser
				if u.Scheme != `https` && u.Scheme != `http` {
					t.Errorf("documentation link %q is not http(s)", dl.Link)
				}
				if u.Host == `` {
					t.Errorf("documentation link %q has no host", dl.Link)
				}
			}
			// an icon is optional, but one that is present has to actually be an SVG
			if md.Icon != `` && !strings.Contains(md.Icon, `<svg`) {
				t.Errorf("icon is %d bytes and is not an SVG", len(md.Icon))
			}
		})
	}
}
