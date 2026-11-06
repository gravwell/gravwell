/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package sqs

import (
	_ "embed" // for the //go:embed directive below, nothing from it is referenced

	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
)

// DocLinks are the places to read about this plugin, shown next to it in a configuration
// interface.  Nothing here is load bearing, a link that goes stale costs a click.
var DocLinks = []dynamic.DocLink{
	{Name: `source`, Link: `https://github.com/gravwell/gravwell/tree/main/hosted/plugins/sqs`},
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

// Icon is this plugin's artwork, read out of sqs.svg at build time.
//
// Embedding rather than reading the file at runtime is what lets the icon travel with a
// single static binary: there is no install layout to get wrong and no path to resolve,
// and an icon that is missing is a build failure rather than a blank square somebody
// notices in production.  The directive has to sit immediately above the declaration, a
// blank line between the two and it is just a comment.
//
//go:embed sqs.svg
var Icon string
