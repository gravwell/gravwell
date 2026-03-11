package files_test

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

const (
	username, password string = "admin", "changeme"
	server             string = "localhost:8080"
)

func TestCreateListDownload(t *testing.T) {
	tDir := t.TempDir()
	t.Setenv("GRAVWELL_PASSWORD", password)
	meta := []string{"--insecure", "-x", "-u", username, "--server=" + server}

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
	var fileID string
	{
		// create a file to write results to
		resultPath := path.Join(tDir, t.Name()+"list.txt")
		// execute spins up singletons for us
		if ec := tree.Execute(append(meta, []string{"files", "list",
			"--csv",
			"-o", resultPath,
			"--columns", "UID,Name,Size",
		}...)); ec != 0 {
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
		t.Log("files:\n", rows, "\n")
		// identify the Name column
		nameColIdx := slices.Index(rows[0], "Name")
		if nameColIdx == -1 {
			t.Fatal("failed to identify \"Name\" column")
		}
		sizeColIdx := slices.Index(rows[0], "Size") // TODO why cannot we not --columns=Size?
		if sizeColIdx == -1 {
			t.Fatal("failed to identify \"Size\" column")
		}
		idColIdx := slices.Index(rows[0], "UID")
		if idColIdx == -1 {
			t.Fatal("failed to identify \"UID\" column")
		}
		for i := 1; i < len(rows); i++ {
			row := rows[i]
			if row[nameColIdx] != fileName {
				continue
			}
			reportedSize, err := strconv.ParseInt(row[sizeColIdx], 10, 64)
			if err != nil {
				t.Errorf("failed to parse %s into an int: %v", row[sizeColIdx], err)
			}
			if reportedSize != fileSize {
				t.Fatal("incorrect size", testsupport.ExpectedActual(fileSize, reportedSize))
			}
			fileID = row[idColIdx]
			break
		}
	}
	// redownload the file
	{
		resultPath := filePath + ".redown.txt"
		t.Logf("downloading file %v", fileID)
		// execute spins up singletons for us
		if ec := tree.Execute(append(meta, []string{"files", "download",
			"-o", resultPath,
			fileID}...)); ec != 0 {
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
