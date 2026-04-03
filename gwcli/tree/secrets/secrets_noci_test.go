//go:build noci

/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package secrets_test

import (
	"encoding/csv"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
)

const (
	username, password string = "admin", "changeme"
)

var (
	server string
	meta   []string
)

func init() {
	server = testsupport.Server()
	meta = []string{"--insecure", "-x", "-u", username, "--server=" + server}
}

// Check that we can 1) create a new secret, 2) confirm we created that secret, 3) alter that secret, and 4) download that secret
func TestCreateEditDownload(t *testing.T) {
	t.Setenv("GRAVWELL_PASSWORD", password)

	var (
		secretName   = randomdata.SillyName()
		secretDesc   = "from " + t.Name()
		secretLabels = []string{"lbl1", "otherlbl"}
		secretValue  = randomdata.Address()
	)

	// execute spins up singletons for us
	if ec := tree.Execute(append(meta, []string{"secrets", "create",
		"-n", secretName,
		"-d", secretDesc,
		"-v", secretValue,
		"--labels", strings.Join(secretLabels, ","),
	}...)); ec != 0 {
		t.Error("bad error code: ", ec)
	}

	// check that list pulls back the new secret
	secretID, desc, lbls := listForItem(t, secretName)
	// validate
	if desc != secretDesc {
		t.Error("retrieved incorrect description", testsupport.ExpectedActual(secretDesc, desc))
	}
	if !testsupport.SlicesUnorderedEqual(lbls, secretLabels) {
		t.Error("assigned labels do not match given labels", testsupport.ExpectedActual(secretLabels, lbls))
	}

	// check that we can alter one of the properties
	{ // disabled due to issues#2187
		newDesc := "altered"
		if ec := tree.Execute(append(meta, []string{"secrets", "edit", "-i", secretID,
			"--description=" + newDesc,
		}...)); ec != 0 {
			t.Error("bad error code: ", ec)
		}
		id, desc, lbls := listForItem(t, secretName)
		if id != secretID {
			t.Error("incorrect secret ID", testsupport.ExpectedActual(secretID, id))
		}
		if desc != newDesc {
			t.Error("incorrect description", testsupport.ExpectedActual(desc, newDesc))
		}
		if !testsupport.SlicesUnorderedEqual(lbls, secretLabels) {
			t.Error("incorrect labels set by edit", testsupport.ExpectedActual(lbls, secretLabels))
		}
	}

	// check that we can delete the secret
	{
		t.Logf("deleting secret %v", secretID)
		// execute spins up singletons for us
		if ec := tree.Execute(append(meta, []string{"secrets", "delete", "--id", secretID}...)); ec != 0 {
			t.Error("bad error code: ", ec)
		}
	}

}

// listForItem executes "list", identifies a row with the given name, and returns its details.
func listForItem(t *testing.T, name string) (id, description string, labels []string) {
	t.Helper()
	resultPath := path.Join(t.TempDir(), t.Name()+"list.txt")
	// execute spins up singletons for us
	if ec := tree.Execute(append(meta, []string{"secrets", "list",
		"--csv",
		"-o", resultPath,
		"--columns", "CommonFields.ID,CommonFields.Name,CommonFields.Description,CommonFields.Labels",
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
	if len(rows[0]) != 4 {
		t.Fatal("incorrect column count", testsupport.ExpectedActual(4, len(rows[0])))
	}
	for i := 1; i < len(rows); i++ {
		row := rows[i]

		// check if this our row
		if row[1] != name {
			continue
		}
		// fetch data to return
		id = row[0]
		description = row[2]
		labels = strings.Split(strings.Trim(row[3], "[]"), " ") // slice off the brackets and split the labels into an array

		return id, description, labels
	}
	t.Fatalf("found no rows with name %v. Rows: %v", name, rows[1:])
	return
}
