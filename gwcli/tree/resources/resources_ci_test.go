/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package resources_test

import (
	"encoding/csv"
	"os"
	"path"
	"slices"
	"strconv"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
)

const username, password string = "admin", "changeme"

// Check that we can 1) create a new resource, 2) confirm we created that resource, and 3) download that resource
func TestCreateListVerify(t *testing.T) {
	tDir := t.TempDir()
	t.Setenv("GRAVWELL_PASSWORD", password)
	meta := []string{"--insecure", "-x", "-u", username}

	// create a file to upload
	var (
		fileSize int64
		filePath string
	)
	{
		filePath = path.Join(tDir, t.Name()+"create.txt")
		f, err := os.Create(filePath)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(randomdata.RandStringRunes(50))
		f.Sync()
		fi, err := f.Stat()
		if err != nil {
			t.Fatal()
		}
		fileSize = fi.Size()
		f.Close()
	}

	var (
		resourceName string = randomdata.SillyName() + strconv.FormatInt(fileSize, 10)
		resourceDesc string = "from " + t.Name()
	)

	createResource := []string{"resources", "create",
		"-n", resourceName,
		"-d", resourceDesc,
		"-f", filePath,
	}
	// execute spins up singletons for us
	if ec := tree.Execute(append(meta, createResource...)); ec != 0 {
		t.Error("bad error code: ", ec)
	}

	// check that list pulls back the new resource
	var resourceID string
	{
		resultPath := path.Join(tDir, t.Name()+"list.txt")
		listResources := []string{"resources", "list",
			"--csv",
			"-o", resultPath,
		}
		// execute spins up singletons for us
		if ec := tree.Execute(append(meta, listResources...)); ec != 0 {
			t.Error("bad error code: ", ec)
		}
		// check for the macro we created
		f, err := os.Open(resultPath)
		if err != nil {
			t.Error(err)
		}
		rdr := csv.NewReader(f)
		rows, err := rdr.ReadAll()
		if err != nil {
			t.Fatal(err)
		} else if len(rows) < 1 {
			t.Fatal("no rows returned")
		}
		t.Log("resources:\n", rows, "\n")
		// identify the Name column
		nameColIdx := slices.Index(rows[0], "Name")
		if nameColIdx == -1 {
			t.Fatal("failed to identify \"Name\" column")
		}
		sizeColIdx := slices.Index(rows[0], "SizeBytes")
		if sizeColIdx == -1 {
			t.Fatal("failed to identify \"Size\" column")
		}
		idColIdx := slices.Index(rows[0], "SizeBytes")
		if sizeColIdx == -1 {
			t.Fatal("failed to identify \"Size\" column")
		}
		for i := 1; i < len(rows); i++ {
			row := rows[i]
			if row[nameColIdx] != resourceName {
				continue
			}
			reportedSize, err := strconv.ParseInt(row[sizeColIdx], 10, 64)
			if err != nil {
				t.Errorf("failed to parse %s into an int: %v", row[sizeColIdx], err)
			}
			if reportedSize != fileSize {
				t.Fatal("incorrect size", testsupport.ExpectedActual(fileSize, reportedSize))
			}
			resourceID = row[idColIdx]
			break

		}
	}
	// check that we can download the resource
	{
		resultPath := filePath + ".redown.txt"
		// execute spins up singletons for us
		if ec := tree.Execute(append(meta, []string{"resources", "download", "-o", resultPath, resourceID}...)); ec != 0 {
			t.Error("bad error code: ", ec)
		}
		// check the file
		dl, err := os.ReadFile(resultPath)
		if err != nil {
			t.Fatal("failed to read download: ", err)
		}
		orig, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatal("failed to read original file: ", err)
		}
		if string(dl) != string(orig) {
			t.Error(testsupport.ExpectedActual(string(orig), string(dl)))
		}
	}

}
