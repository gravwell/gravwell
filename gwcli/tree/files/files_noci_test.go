//go:build !ci

package files_test

import (
	"encoding/csv"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
)

const (
	username, password string = "admin", "changeme"
	server             string = "localhost:8080"
)

var meta = []string{"--insecure", "-x", "-u", username, "--server=" + server}

func TestCreateListDownload(t *testing.T) {
	tDir := t.TempDir()
	t.Setenv("GRAVWELL_PASSWORD", password)

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
		fileName = randomdata.SillyName() + strconv.FormatInt(fileSize, 10)
		fileDesc = "from " + t.Name()
	)

	{ // create the new userfile
		if ec := tree.Execute(append(meta, []string{"files", "create",
			"-n", fileName,
			"-d", fileDesc,
			"-f", filePath,
		}...)); ec != 0 {
			t.Fatal("bad error code: ", ec)
		}
	}
	// check for the new file
	fileID, desc, lbls := fileDetails(t, fileName, fileSize)
	// validate
	if desc != fileDesc {
		t.Error("retrieved incorrect description", testsupport.ExpectedActual(fileDesc, desc))
	}
	if testsupport.SlicesUnorderedEqual(lbls, []string{}) { // we did not provide any labels
		t.Error("retrieved incorrect description", testsupport.ExpectedActual([]string{}, lbls))
	}

	// check that we can alter one of the properties
	{
		//	if
	}

	// redownload the file
	{
		resultPath := filePath + ".redown.txt"
		t.Logf("downloading file %v", fileID)
		// execute spins up singletons for us
		if ec := tree.Execute(append(meta, []string{"files", "download",
			"-o", resultPath,
			fileID.String()}...)); ec != 0 {
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

func fileDetails(t *testing.T, name string, size int64) (id uuid.UUID, description string, labels []string) {
	// create a file to write results to
	resultPath := path.Join(t.TempDir(), t.Name()+"list.txt")
	if ec := tree.Execute(append(meta, []string{"files", "list",
		"--csv",
		"-o", resultPath,
		"--columns", "ThingUUID,Name,Desc,Size,Labels",
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
	if len(rows) != 5 {
		t.Fatal("incorrect column count", testsupport.ExpectedActual(5, len(rows)))
	}
	t.Log("columns:\n", rows[0], "\n")
	for i := 1; i < len(rows); i++ {
		row := rows[i]

		// check if this is our row
		if row[1] != name {
			continue
		}
		// validate size
		reportedSize, err := strconv.ParseInt(row[3], 10, 64)
		if err != nil {
			t.Errorf("failed to parse %s into an int: %v", row[3], err)
		}
		if reportedSize != size {
			t.Fatal("incorrect size", testsupport.ExpectedActual(size, reportedSize))
		}
		// fetch data to return
		id, err = uuid.Parse(row[0])
		if err != nil {
			t.Fatal(err)
		}
		description = row[2]
		labels = strings.Split(strings.Trim(row[4], "[]"), " ") // slice off the brackets and split the labels into an array

		return id, description, labels
	}
	t.Fatalf("found no rows with name %v. Rows: %v", name, rows[1:])
	return
}
