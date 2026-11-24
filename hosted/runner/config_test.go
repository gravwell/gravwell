/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"path/filepath"
	"testing"

	"github.com/gravwell/gravwell/v4/hosted/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyKeepsEnvironment(t *testing.T) {
	t.Setenv("GRAVWELL_INGEST_SECRET", "envsecret")
	t.Setenv("GRAVWELL_CLEARTEXT_TARGETS", "127.0.0.1:4023")
	c := &cfgType{State: storage.BoltConfig{Path: filepath.Join(t.TempDir(), "state.db")}}
	require.NoError(t, c.Verify())
	assert.Equal(t, "envsecret", c.Ingest_Secret)
	assert.Equal(t, []string{"127.0.0.1:4023"}, c.Cleartext_Backend_Target)
}
