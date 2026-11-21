/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mqtt

import (
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
)

// DocLinks are the places to read about this plugin, shown next to it in a
// configuration interface.  Nothing here is load bearing, a link that goes
// stale costs a click.
var DocLinks = []dynamic.DocLink{
	{Name: `source`, Link: `https://github.com/gravwell/gravwell/tree/main/hosted/plugins/mqtt`},
}

// RunnerMetadata implements the optional dynamic.MetadataProvider interface, which is how
// this plugin's documentation and version reach a webserver: the config type is
// what gets registered, so hanging the description off it means nothing has to be
// threaded through the registration path by hand.
//
// No icon is shipped.  The MQTT logo is a trademark of OASIS and is not ours to
// embed, and an icon is optional, so the field is simply left empty.
func (c *Config) RunnerMetadata() *dynamic.RunnerMetadata {
	return &dynamic.RunnerMetadata{
		Documentation: DocLinks,
		Version:       dynamic.Version(Version),
	}
}
