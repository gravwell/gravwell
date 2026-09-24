//go:build noci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scripts_test

import (
	"encoding/csv"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Check that we can 1) create a new script, 2) confirm we created it, 3) alter it, and 4) delete it.
func TestCreateEditDelete(t *testing.T) {
	meta := testsupport.MetaArgs(t, false, testsupport.WithDefaults())
	tDir := t.TempDir()

	// create a script file to upload
	scriptPath := path.Join(tDir, t.Name()+".anko")
	require.Nil(t, os.WriteFile(scriptPath, []byte("println(\"Hello World\")\n"), 0644))

	var (
		scriptName   = randomdata.SillyName()
		scriptDesc   = "from " + t.Name()
		scriptLabels = []string{"lbl1", "otherlbl"}
	)

	scriptID := createScript(t, meta, []string{"automation", "scripts", "create",
		"-n", scriptName,
		"-d", scriptDesc,
		"--path", scriptPath,
		"--labels", strings.Join(scriptLabels, ","),
		"--language", "anko",
		"--frequency", "* * * * *",
	})

	// check that list pulls back the new script
	id, desc, lbls, lang := listForItem(t, meta, scriptName)
	assert.Equal(t, scriptID, id, "incorrect script ID")
	assert.Equal(t, scriptDesc, desc, "incorrect script description")
	assert.Equal(t, "anko", lang, "incorrect script language")
	if !testsupport.SlicesUnorderedEqual(lbls, scriptLabels) {
		t.Error("assigned labels do not match given labels", testsupport.ExpectedActual(scriptLabels, lbls))
	}

	// check that we can alter one of the properties
	{
		newDesc := "altered"
		var sbErr strings.Builder
		if ec := tree.Execute(append(meta, []string{"automation", "scripts", "edit", "-i", scriptID,
			"--description=" + newDesc,
		}...), tree.ExecuteOptions{Stderr: &sbErr}); ec != 0 {
			t.Fatal("bad error code. STDERR: ", sbErr.String())
		}
		id, desc, _, _ := listForItem(t, meta, scriptName)
		assert.Equal(t, scriptID, id, "incorrect script ID")

		assert.Equal(t, newDesc, desc, "incorrect script description")

	}

	// check that we can delete the script
	{
		t.Logf("deleting script %v", scriptID)
		var sbErr strings.Builder
		if ec := tree.Execute(append(meta, []string{"automation", "scripts", "delete", scriptID}...), tree.ExecuteOptions{Stderr: &sbErr}); ec != 0 {
			t.Fatal("bad error code. STDERR: ", sbErr.String())
		}
	}
}

// createScript runs the given create-flavoured args and returns the ID of the newly created script.
func createScript(t *testing.T, meta, args []string) (ID string) {
	t.Helper()
	var sbOut, sbErr strings.Builder
	if ec := tree.Execute(append(meta, args...), tree.ExecuteOptions{Stdout: &sbOut, Stderr: &sbErr}); ec != 0 {
		t.Fatal("bad error code. STDERR: ", sbErr.String())
	}
	ID = testsupport.FindID(sbOut.String())
	if ID == "" {
		t.Fatalf("could not find ID in output: %s", sbOut.String())
	}
	return ID
}

// Check that we can toggle a script's backfill setting on and back off again.
func TestBackfillToggle(t *testing.T) {
	meta := testsupport.MetaArgs(t, false, testsupport.WithDefaults())
	tDir := t.TempDir()

	scriptPath := path.Join(tDir, t.Name()+".anko")
	require.Nil(t, os.WriteFile(scriptPath, []byte("println(\"Hello World\")\n"), 0644))

	scriptID := createScript(t, meta, []string{"automation", "scripts", "create",
		"-n", randomdata.SillyName(),
		"-d", "from " + t.Name(),
		"--path", scriptPath,
		"--language", "anko",
		"--frequency", "* * * * *",
	})
	t.Cleanup(func() {
		var sbErr strings.Builder
		if ec := tree.Execute(append(meta, []string{"automation", "scripts", "delete", scriptID}...), tree.ExecuteOptions{Stderr: &sbErr}); ec != 0 {
			t.Errorf("failed to clean up script %v: %v", scriptID, sbErr.String())
		}
	})

	// backfill should start disabled
	if bf := getBackfillEnabled(t, meta, scriptID); bf {
		t.Fatal("expected backfill to start disabled")
	}

	// enable backfill
	{
		var sbErr strings.Builder
		if ec := tree.Execute(append(meta, []string{"automation", "scripts", "backfill", scriptID, "--enable"}...), tree.ExecuteOptions{Stderr: &sbErr}); ec != 0 {
			t.Fatal("bad error code. STDERR: ", sbErr.String())
		}
		if bf := getBackfillEnabled(t, meta, scriptID); !bf {
			t.Error("expected backfill to be enabled after --enable")
		}
	}

	// disable backfill
	{
		var sbErr strings.Builder
		if ec := tree.Execute(append(meta, []string{"automation", "scripts", "backfill", scriptID, "--disable"}...), tree.ExecuteOptions{Stderr: &sbErr}); ec != 0 {
			t.Fatal("bad error code. STDERR: ", sbErr.String())
		}
		if bf := getBackfillEnabled(t, meta, scriptID); bf {
			t.Error("expected backfill to be disabled after --disable")
		}
	}
}

// getBackfillEnabled executes "list" against a single script by ID and returns its BackfillEnabled value.
func getBackfillEnabled(t *testing.T, meta []string, id string) bool {
	t.Helper()
	resultPath := path.Join(t.TempDir(), t.Name()+"backfill.txt")
	var sbErr strings.Builder
	if ec := tree.Execute(append(meta, []string{"automation", "scripts", "list",
		"--id", id,
		"--csv",
		"-o", resultPath,
		"--columns=ID,BackfillEnabled",
	}...), tree.ExecuteOptions{Stderr: &sbErr}); ec != 0 {
		t.Fatal("bad error code. STDERR: ", sbErr.String())
	}

	f, err := os.Open(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	rdr := csv.NewReader(f)
	rows, err := rdr.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected exactly one data row for script %v, got %v", id, rows)
	}
	if rows[1][0] != id {
		t.Fatalf("unexpected script ID returned. %v", testsupport.ExpectedActual(id, rows[1][0]))
	}
	bf, err := strconv.ParseBool(rows[1][1])
	if err != nil {
		t.Fatalf("failed to parse BackfillEnabled value %q: %v", rows[1][1], err)
	}
	return bf
}

// listForItem executes "list", identifies a row with the given name, and returns its details.
func listForItem(t *testing.T, meta []string, name string) (id, description string, labels []string, language string) {
	t.Helper()
	resultPath := path.Join(t.TempDir(), t.Name()+"list.txt")
	var sbErr strings.Builder
	if ec := tree.Execute(append(meta, []string{"automation", "scripts", "list",
		"--csv",
		"-o", resultPath,
		"--columns", "CommonFields.ID,CommonFields.Name,CommonFields.Description,CommonFields.Labels,ScriptLanguage",
	}...), tree.ExecuteOptions{Stderr: &sbErr}); ec != 0 {
		t.Fatal("bad error code. STDERR: ", sbErr.String())
	}
	// slurp the file we wrote to
	var rows [][]string
	{
		f, err := os.Open(resultPath)
		if err != nil {
			t.Error(err)
		}
		rdr := csv.NewReader(f)
		rows, err = rdr.ReadAll()
		if err != nil {
			t.Fatal(err)
		} else if len(rows) < 1 {
			t.Fatal("no rows returned")
		}
	}
	t.Log("columns:\n", rows[0], "\n")
	if len(rows[0]) != 5 {
		t.Fatal("incorrect column count", testsupport.ExpectedActual(5, len(rows[0])))
	}
	for i := 1; i < len(rows); i++ {
		row := rows[i]

		// check if this is our row
		if row[1] != name {
			continue
		}
		// fetch data to return
		id = row[0]
		description = row[2]
		labels = strings.Split(strings.Trim(row[3], "[]"), " ") // slice off the brackets and split the labels into an array
		language = row[4]

		return id, description, labels, language
	}
	t.Fatalf("found no rows with name %v. Rows: %v", name, rows[1:])
	return
}
