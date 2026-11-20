/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"net/http"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/utils/clientip"
	"github.com/gravwell/gravwell/v4/utils/trustedproxy"
)

// clientSource resolves the client of each request for every listener. The
// legacy warning is rate limited across all listeners, so one clientSource is
// shared by all of them.
type clientSource struct {
	resolver   clientip.Resolver // set once at startup, read without a lock
	legacyWarn *clientip.LegacyWarner
}

func newClientSource(cr clientip.Resolver, lg log.IngestLogger) (*clientSource, error) {
	if lg == nil {
		return nil, errors.New("nil logger")
	}

	return &clientSource{
		resolver:   cr,
		legacyWarn: clientip.NewLegacyWarner(lg),
	}, nil
}

// client resolves the client of r with the configured resolver.
func (cs *clientSource) client(r *http.Request) trustedproxy.Client {
	return cs.resolver.Resolve(r, cs.legacyWarn)
}

// srcKVs returns the client KVs of a request under the name src, followed by
// extra, for log lines about that request.
func srcKVs(c trustedproxy.Client, extra ...rfc5424.SDParam) []rfc5424.SDParam {
	return append(clientip.KVs(`src`, c), extra...)
}
