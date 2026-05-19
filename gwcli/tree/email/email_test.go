/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package email_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmailConfigurationGetSet(t *testing.T) {
	// destroy any pre-existing configuration
	t.Run("delete", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"email", "delete",
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
	})
	if t.Failed() {
		t.FailNow()
	}
	t.Run("get empty configuration", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"email", "show",
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
		require.Equal(t, "you do not have a mail server configured", strings.TrimSpace(strings.ToLower(stdout.String())))
	})
	if t.Failed() {
		t.FailNow()
	}
	emlServer := "server"
	emlPort := 666
	emlUsername := "user"
	// tls is also set
	t.Run("set configuration", func(t *testing.T) {
		var stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"email", "configure",
				"--email-server="+emlServer,
				"--email-port="+strconv.FormatInt(int64(emlPort), 10),
				"--email-username="+emlUsername,
				"--tls",
			),
			nil,
			&stderr))
		require.Empty(t, stderr.String())
	})
	if t.Failed() {
		t.FailNow()
	}
	t.Run("get configuration", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"email", "show",
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
		// TODO check output
	})
}
