//go:build noci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package skills_test

import (
	"encoding/json"
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

var ( // set by init
	server string
	meta   []string
)

func init() {
	server = testsupport.Server()
	meta = []string{"--insecure", "-x", "-u", username, "--server=" + server}
}

// Upload a skill, find it in list, edit its metadata, download it, replace its body, and delete it.
func TestUploadEditDownloadReplaceDelete(t *testing.T) {
	tDir := t.TempDir()
	t.Setenv("GRAVWELL_PASSWORD", password)

	var (
		skillName   = "gwcli-" + randomdata.SillyName()
		skillDesc   = "from " + t.Name()
		skillLabels = []string{"lbl1", "otherlbl"}
		body        = "# " + skillName + "\n\n" + randomdata.Paragraph() + "\n"
	)
	filePath := path.Join(tDir, "skill.md")
	if err := os.WriteFile(filePath, []byte("---\nname: "+skillName+"\ndescription: "+skillDesc+"\n---\n"+body), 0600); err != nil {
		t.Fatal(err)
	}

	execute(t, "ai", "skills", "upload", "-f", filePath, "--labels", strings.Join(skillLabels, ","))

	// check that list pulls back the new skill, named and described by its front matter
	row := listForItem(t, skillName)
	skillID := row.ID
	if row.Description != skillDesc {
		t.Error("retrieved incorrect description", testsupport.ExpectedActual(skillDesc, row.Description))
	}
	if !testsupport.SlicesUnorderedEqual(row.Labels, skillLabels) {
		t.Error("assigned labels do not match given labels", testsupport.ExpectedActual(skillLabels, row.Labels))
	}
	if row.BodyBytes != len(body) {
		t.Error("incorrect body size", testsupport.ExpectedActual(len(body), row.BodyBytes))
	}

	// check that we can alter its metadata
	newDesc, newLabels := "altered", []string{"edited"}
	execute(t, "ai", "skills", "edit", "-i", skillID, "--description="+newDesc, "--labels="+strings.Join(newLabels, ","))
	row = listForItem(t, skillName)
	if row.ID != skillID {
		t.Error("incorrect skill ID", testsupport.ExpectedActual(skillID, row.ID))
	}
	if row.Description != newDesc {
		t.Error("incorrect description", testsupport.ExpectedActual(newDesc, row.Description))
	}
	if !testsupport.SlicesUnorderedEqual(row.Labels, newLabels) {
		t.Error("incorrect labels set by edit", testsupport.ExpectedActual(newLabels, row.Labels))
	}

	// check that we can download it, front matter first
	{
		resultPath := path.Join(tDir, "download.md")
		execute(t, "ai", "skills", "download", "-o", resultPath, skillID)
		dl, err := os.ReadFile(resultPath)
		if err != nil {
			t.Fatal("failed to read download: ", err)
		}
		want := "---\nname: " + skillName + "\ndescription: " + newDesc + "\n---\n" + body
		if string(dl) != want {
			t.Error(testsupport.ExpectedActual(want, string(dl)))
		}
	}

	// check that replace swaps the body, and keeps the metadata when the file has no front matter
	{
		newBody := "replaced " + randomdata.SillyName() + "\n"
		replacePath := path.Join(tDir, "replace.md")
		if err := os.WriteFile(replacePath, []byte(newBody), 0600); err != nil {
			t.Fatal(err)
		}
		execute(t, "ai", "skills", "replace", "-f", replacePath, skillID)
		out := execute(t, "ai", "skills", "download", skillID)
		want := "---\nname: " + skillName + "\ndescription: " + newDesc + "\n---\n" + newBody
		if strings.TrimSpace(out) != strings.TrimSpace(want) {
			t.Error(testsupport.ExpectedActual(want, out))
		}
	}

	execute(t, "ai", "skills", "delete", skillID)
	if findRow(listRows(t), skillName) != nil {
		t.Errorf("skill %v still listed after delete", skillID)
	}
}

// execute runs gwcli with the given arguments, failing the test on a non-zero exit, and returns stdout.
func execute(t *testing.T, args ...string) string {
	t.Helper()
	var sbOut, sbErr strings.Builder
	if ec := tree.Execute(append(meta, args...), tree.ExecuteOptions{Stdout: &sbOut, Stderr: &sbErr}); ec != 0 {
		t.Fatalf("%v: bad error code %d. STDERR: %s", args, ec, sbErr.String())
	}
	return sbOut.String()
}

const listColumns = "CommonFields.ID,CommonFields.Name,CommonFields.Description,CommonFields.Labels,BodyBytes"

// listedSkill is a row of "list --json" with listColumns.
type listedSkill struct {
	ID, Name, Description string
	Labels                []string
	BodyBytes             int
}

// listRows executes "list" and returns its rows.
// It reads JSON rather than CSV, as the CSV output does not quote descriptions that contain commas.
func listRows(t *testing.T) []listedSkill {
	t.Helper()
	resultPath := path.Join(t.TempDir(), "list.json")
	execute(t, "ai", "skills", "list", "--json", "-o", resultPath, "--columns", listColumns)
	b, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var rows []listedSkill
	if err := json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func findRow(rows []listedSkill, name string) *listedSkill {
	for i := range rows {
		if rows[i].Name == name {
			return &rows[i]
		}
	}
	return nil
}

// listForItem executes "list" and returns the row with the given name.
func listForItem(t *testing.T, name string) listedSkill {
	t.Helper()
	rows := listRows(t)
	row := findRow(rows, name)
	if row == nil {
		t.Fatalf("found no rows with name %v. Rows: %v", name, rows)
	}
	return *row
}
