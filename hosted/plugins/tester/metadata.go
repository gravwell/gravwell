/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package tester

import (
	_ "embed" // for the //go:embed directive below, nothing from it is referenced

	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
)

var DocLinks = []dynamic.DocLink{
	{Name: `source`, Link: `https://github.com/gravwell/gravwell/tree/main/hosted/plugins/tester`},
}

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

// Icon is this plugin's artwork, read out of tester.svg at build time.
//
//go:embed tester.svg
var Icon string
