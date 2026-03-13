//go:build !ci

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
	"strconv"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
)

const (
	username, password string = "admin", "changeme"
	server             string = "localhost:8080"
)

var meta = []string{"--insecure", "-x", "-u", username, "--server=" + server}

// Check that we can 1) create a new resource, 2) confirm we created that resource, and 3) download that resource
func TestCreateEditDownload(t *testing.T) {
	tDir := t.TempDir()
	t.Setenv("GRAVWELL_PASSWORD", password)
	meta := []string{"--insecure", "-x", "-u", username, "--server=" + server}

	// create a file to upload
	var (
		resourceSize int64
		filePath     string
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
		resourceSize = fi.Size()
		f.Close()
	}

	var (
		resourceName   = randomdata.SillyName() + strconv.FormatInt(resourceSize, 10)
		resourceDesc   = "from " + t.Name()
		resourceLabels = []string{"lbl1", "otherlbl"}
	)

	createResource := []string{"resources", "create",
		"-n", resourceName,
		"-d", resourceDesc,
		"-f", filePath,
		"--labels", strings.Join(resourceLabels, ","),
	}
	// execute spins up singletons for us
	if ec := tree.Execute(append(meta, createResource...)); ec != 0 {
		t.Error("bad error code: ", ec)
	}

	// check that list pulls back the new resource
	resourceID, desc, lbls := listForItem(t, resourceName, resourceSize)
	// validate
	if desc != resourceDesc {
		t.Error("retrieved incorrect description", testsupport.ExpectedActual(resourceDesc, desc))
	}
	if !testsupport.SlicesUnorderedEqual(lbls, resourceLabels) {
		t.Error("assigned labels do not match given labels", testsupport.ExpectedActual(resourceLabels, lbls))
	}

	// check that we can alter one of the properties
	/*{ // disabled due to issues#2187
		newDesc := "altered"
		if ec := tree.Execute(append(meta, []string{"resources", "edit", "-i", resourceID,
			"--description=" + newDesc,
		}...)); ec != 0 {
			t.Error("bad error code: ", ec)
		}
		id, desc, lbls := listForItem(t, resourceName, resourceSize)
		if id != resourceID {
			t.Error("incorrect resource ID", testsupport.ExpectedActual(resourceID, id))
		}
		if desc != newDesc {
			t.Error("incorrect description", testsupport.ExpectedActual(desc, newDesc))
		}
		if !testsupport.SlicesUnorderedEqual(lbls, resourceLabels) {
			t.Error("incorrect labels set by edit", testsupport.ExpectedActual(lbls, resourceLabels))
		}
	}*/

	// check that we can download the resource
	{
		resultPath := filePath + ".redown.txt"
		t.Logf("downloading resource %v", resourceID)
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

// listForItem executes "list", identifies a row with the given name, and returns its details.
func listForItem(t *testing.T, name string, size int64) (id, description string, labels []string) {
	resultPath := path.Join(t.TempDir(), t.Name()+"list.txt")
	// execute spins up singletons for us
	if ec := tree.Execute(append(meta, []string{"resources", "list",
		"--csv",
		"-o", resultPath,
		"--columns", "CommonFields.ID,CommonFields.Name,CommonFields.Description,Size,CommonFields.Labels",
	}...)); ec != 0 {
		t.Error("bad error code: ", ec)
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

		// check if this our row
		if row[1] != name {
			continue
		}
		// validate size
		reportedSize, err := strconv.ParseInt(row[3], 10, 64)
		if err != nil {
			t.Errorf("failed to parse %s into an int: %v", row[2], err)
		}
		if reportedSize != size {
			t.Fatal("incorrect size", testsupport.ExpectedActual(size, reportedSize))
		}
		// fetch data to return
		id = row[0]
		description = row[2]
		labels = strings.Split(strings.Trim(row[4], "[]"), " ") // slice off the brackets and split the labels into an array

		return id, description, labels
	}
	t.Fatalf("found no rows with name %v. Rows: %v", name, rows[1:])
	return
}
