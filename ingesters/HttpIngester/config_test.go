/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gravwell/gravwell/v4/ingest/attach"
	"github.com/stretchr/testify/require"
)

const attachTestConfig = `
[Global]
	Ingest-Secret = IngestSecrets
	Cleartext-Backend-Target=127.0.0.1:4023
	Bind=":8080"

[Attach]
	HTTPTouched="$NOW"
	HTTPHostname="$HOSTNAME"
	last="http"

[Listener "test"]
	URL="/data"
	Tag-Name=test
`

// tests that the Attach section is read as raw by gcfg.
func TestGetConfigAttach(t *testing.T) {
	// write out test config
	pth := filepath.Join(t.TempDir(), "http_ingester.conf")
	require.NoError(t, os.WriteFile(pth, []byte(attachTestConfig), 0600))
	// attempt to reload it
	c, err := GetConfig(pth, ``)
	require.NoError(t, err)
	require.Equal(t, attach.AttachConfig{
		"HTTPTouched":  "$NOW",
		"HTTPHostname": "$HOSTNAME",
		"last":         "http",
	}, c.Attach)
}
