/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package okta

import "github.com/gravwell/gravwell/v4/ingest/config/dynamic"

// DocLinks are the places to read about this plugin, shown next to it in a configuration
// interface.  Nothing here is load bearing, a link that goes stale costs a click.
var DocLinks = []dynamic.DocLink{
	{Name: `source`, Link: `https://github.com/gravwell/gravwell/tree/main/hosted/plugins/okta`},
}

// Icon is this plugin's artwork.  It is empty until someone draws one, which is allowed:
// an interface that has no icon for a plugin falls back to its name.  To add one, drop an
// SVG in this directory and embed it the way the tester plugin does.
var Icon string

// RunnerMetadata implements the optional dynamic.MetadataProvider interface, which is how
// this plugin's icon, documentation and version reach a webserver: the config type is
// what gets registered, so hanging the description off it means nothing has to be
// threaded through the registration path by hand.
func (c *Config) RunnerMetadata() *dynamic.RunnerMetadata {
	return &dynamic.RunnerMetadata{
		Icon:          Icon,
		Documentation: DocLinks,
		Version:       dynamic.Version(Version),
	}
}
