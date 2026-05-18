package admin_test

import (
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogLevelGetSet(t *testing.T) {
	var curLevel string
	t.Run("get current log level", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"admin", "log-level",
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
		// parse out current log level
		_, after, found := strings.Cut(stdout.String(), "current log level: ")
		require.True(t, found)
		curLevel = strings.TrimSpace(after)
	})
	var setLevel = "info"
	t.Run("change log level", func(t *testing.T) {
		if setLevel == strings.ToLower(curLevel) { // don't want to set to the current level
			setLevel = "warn"
		}

		var stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"admin", "log-level", "--set="+setLevel,
			),
			nil,
			&stderr))
		require.Empty(t, stderr.String())
	})
	t.Run("get updated log level", func(t *testing.T) {
		var stdout, stderr strings.Builder
		assert.Zero(t, tree.Execute(
			append(
				testsupport.MetaArgs(t, false, testsupport.WithDefaults()),
				"admin", "log-level",
			),
			&stdout,
			&stderr))
		require.Empty(t, stderr.String())
		// parse out current log level
		_, after, found := strings.Cut(stdout.String(), "current log level: ")
		require.True(t, found)
		curLevel = strings.ToLower(strings.TrimSpace(after))
		require.Equal(t, setLevel, curLevel)
	})
}
