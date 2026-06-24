//go:build noci

package kits_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
	"github.com/stretchr/testify/require"
)

const (
	username, password string = "admin", "changeme"
)

func TestBuildKit(t *testing.T) {
	meta := testsupport.MetaArgs(t, false, testsupport.WithDefaults())
	t.Setenv("GRAVWELL_PASSWORD", password)

	var itemArguments []string

	// macros
	itemArguments = append(itemArguments, "--macros",
		createAsset(t, meta, []string{"macros", "create", "--name", randomdata.FullName(1), "--expansion=mymacro"}))

	// TODO: Create two resources and include their IDs
	// TODO: Create an extractor and include its ID
	// TODO: Create an file and include its ID
	// TODO: Create an saved query and include its ID

	t.Run("random elements with download", func(t *testing.T) {
		kitName := "TestKit_" + randomdata.RandStringRunes(8)
		kitID := "test-kit-id-" + randomdata.RandStringRunes(8)
		readme := "This is a test kit readme"
		version := "1.2.3"

		// compose the final set of arguments
		dlPath := filepath.Join(t.ArtifactDir(), "buildkit_test"+randomdata.Digits(6)+".kit")
		t.Log("download path: ", dlPath)
		args := append(meta,
			"kits", "build",
			"--download", dlPath,
			"--name", kitName,
			"--kit-id", kitID,
			"--readme", readme,
			"--kit-version", version,
		)
		args = append(args, itemArguments...)

		var sbErr strings.Builder
		ec := tree.Execute(args, nil, &sbErr)
		require.Zero(t, ec, "kits build failed: %s", sbErr.String())
	})
}

var rgxID = regexp.MustCompile(`\(ID:(.*)\)`)

func createAsset(t *testing.T, meta, args []string) (ID string) {
	var sbOut, sbErr strings.Builder
	require.Zero(t, tree.Execute(append(meta, args...), &sbOut, &sbErr), sbErr.String())
	match := rgxID.FindStringSubmatch(sbOut.String())
	require.NotEmpty(t, match, "could not find ID in output: %s", sbOut.String())
	if len(match) < 2 {
		require.FailNow(t, "regex match failed to capture group")
	}
	ID = strings.TrimSpace(match[1])
	require.NotEmpty(t, ID)
	t.Log(args, ": ID: ", ID)
	return ID
}
