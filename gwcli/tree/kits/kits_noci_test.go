//go:build noci

package kits_test

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAndUpload(t *testing.T) {
	meta := testsupport.MetaArgs(t, false, testsupport.WithDefaults())
	tDir := t.TempDir()

	var (
		macroID     string
		resourceIDs []string
		extractorID string
	)

	// macro
	macroID = createAsset(t, meta, []string{"macros", "create", "--name", randomdata.RandStringRunes(5), "--expansion=mymacro"})
	// resources
	{
		r1Contents := randomdata.Day()
		r2Contents := randomdata.Day()
		r1ContentsPath := filepath.Join(tDir, "resource1")
		r2ContentsPath := filepath.Join(tDir, "resource2")
		require.Nil(t, os.WriteFile(r1ContentsPath, []byte(r1Contents), 0755))
		require.Nil(t, os.WriteFile(r2ContentsPath, []byte(r2Contents), 0755))
		resourceIDs = make([]string, 2)
		resourceIDs[0] = createAsset(t, meta, []string{"resource", "create", "--name", randomdata.FullName(2), "--path", r1ContentsPath})
		resourceIDs[1] = createAsset(t, meta, []string{"resource", "create", "--name", randomdata.FullName(2), "--path", r1ContentsPath})
	}
	// extractor
	var sbErr strings.Builder
	require.Zero(t, tree.Execute(append(meta, "axs", "clear", "csv"), nil, &sbErr), sbErr.String())
	extractorID = createAsset(t, meta, []string{"extractors", "create",
		"--name", "csvAX",
		"--module", "csv",
		"--module-parameters", "ts, name, id, guid, src, srcport, dst, dstport, data, country, city, hash",
		"--tags=csv",
	})

	kitPath := filepath.Join(t.ArtifactDir(), "buildkit_test"+randomdata.Digits(6)+".kit")
	t.Log("download path: ", kitPath)

	kitName := "TestKit_" + randomdata.RandStringRunes(8)
	kitID := "test.kit.id." + randomdata.RandStringRunes(8)
	kitReadme := "This is a test kit readme"
	kitVersion := "2"

	success := t.Run("generate a kit with the set of new items", func(t *testing.T) {
		// compose the final set of arguments
		args := append(meta,
			"kits", "build",
			"--download", kitPath,
			"--name", kitName,
			"--kit-id", kitID,
			"--readme", kitReadme,
			"--kit-version", kitVersion,
		)
		// compile item args
		args = append(args, "--macros", macroID, "--resources", strings.Join(resourceIDs, ","), "--axs", extractorID)

		t.Log("final arg string: ", args)
		var sbErr strings.Builder
		require.Zero(t, tree.Execute(args, nil, &sbErr), sbErr.String())
		// check that the kit now exists at our path
		fi, err := os.Stat(kitPath)
		require.Nil(t, err)
		t.Logf("found file %v", fi.Name())
		if fi.Size() < 1 {
			t.Fatal("kit file is empty")
		}
	})
	if !success { // future tests rely on the kit existing
		t.FailNow()
	}

	{ // destroy the assets we created
		var sbOut, sbErr strings.Builder
		if !assert.Zero(t, tree.Execute(append(meta, "macros", "delete", macroID), &sbOut, &sbErr)) {
			success = false
		}
		if !assert.Zero(t, tree.Execute(append(meta, "resource", "delete", resourceIDs[0]), &sbOut, &sbErr)) {
			success = false
		}
		if !assert.Zero(t, tree.Execute(append(meta, "resource", "delete", resourceIDs[1]), &sbOut, &sbErr)) {
			success = false
		}
		if !assert.Zero(t, tree.Execute(append(meta, "extractors", "delete", extractorID), &sbOut, &sbErr)) {
			success = false
		}
		if !success {
			t.Fatalf("failed to purge items. ---Stdout: %v\n---Stderr: %v", sbOut, sbErr)
		}
	}

	t.Run("identify the kit build request", func(t *testing.T) {
		var sbOut, sbErr strings.Builder
		require.Zero(t,
			tree.Execute(append(meta, "kits", "build-requests", "--csv", "--columns=KitID,Name,Readme,KitVersion,Macros,Resources,Extractors"),
				&sbOut, &sbErr), sbErr.String())
		require.Empty(t, sbErr.String())
		rdr := csv.NewReader(strings.NewReader(sbOut.String()))
		hdr, err := rdr.Read()
		require.Nil(t, err)
		require.Len(t, hdr, 7)
		rows, err := rdr.ReadAll()
		require.Nil(t, err)
		// find our request
		for _, row := range rows {
			if row[0] == kitID && row[1] == kitName && row[2] == kitReadme && row[3] == kitVersion {
				t.Log("located build request: ", row)
				// test that all assigned resources are there
				assert.Equal(t, macroID, strings.Trim(row[4], "[]"))
				assert.ElementsMatch(t, resourceIDs, strings.Split(strings.Trim(row[5], "[]"), " "))
				assert.Equal(t, extractorID, strings.Trim(row[6], "[]"))
			}
		}
	})
}

func createAsset(t *testing.T, meta, args []string) (ID string) {
	t.Helper()
	var sbOut, sbErr strings.Builder
	require.Zero(t, tree.Execute(append(meta, args...), &sbOut, &sbErr), sbErr.String())
	ID = testsupport.FindID(sbOut.String())
	require.NotEmpty(t, ID, "could not find ID in output: %s", sbOut.String())
	t.Log(args, ": ID: ", ID)
	return ID
}
