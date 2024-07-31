// Tests from a complete-program perspective, confirming consistent input begets
// reliable output

package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"gwcli/connection"
	"gwcli/tree"
	"io"
	"math/rand"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	grav "github.com/gravwell/gravwell/v3/client"
	"github.com/gravwell/gravwell/v3/utils/weave"
)

const ( // mock credentials
	user     = "admin"
	password = "changeme"
	server   = "localhost:80"
)

var realStderr, mockStderr, realStdout, mockStdout *os.File

//#region non-interactive

func TestNonInteractive(t *testing.T) {
	defer restoreIO() // each test should result before checking results, but ensure a deferred restore

	realStdout = os.Stdout
	realStderr = os.Stderr

	// connect to the server for manual calls
	testclient, err := grav.NewOpts(grav.Opts{Server: server, UseHttps: false, InsecureNoEnforceCerts: true})
	if err != nil {
		panic(err)
	}
	if err = testclient.Login(user, password); err != nil {
		panic(err)
	}

	// need to reset the client used by gwcli between runs
	connection.End()
	connection.Client = nil

	t.Run("tools macros list --csv", func(t *testing.T) {
		// generate results manually, for comparison
		myInfo, err := testclient.MyInfo()
		if err != nil {
			panic(err)
		}
		macros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			panic(err)
		}
		columns := []string{"UID", "Global", "Name"}
		want := weave.ToCSV(macros, columns)

		// prepare IO
		stdoutData, stderrData, err := mockIO()
		if err != nil {
			restoreIO()
			panic(err)
		}

		args := strings.Split("-u admin -p changeme --insecure --script tools macros list --csv --columns=UID,Global,Name", " ")

		// run the test body
		errCode := tree.Execute(args)
		restoreIO()
		if errCode != 0 {
			t.Errorf("non-zero error code: %v", errCode)
		}
		results := <-stdoutData
		resultsErr := <-stderrData
		if resultsErr != "" {
			t.Errorf("non-empty stderr:\n(%v)", resultsErr)
		}

		// compare against expected
		if strings.TrimSpace(results) != strings.TrimSpace(want) {
			t.Errorf("output mismatch\nwant:\n(%v)\ngot:\n(%v)\n", want, results)
		}
	})

	// need to reset the client used by gwcli between runs
	connection.End()
	connection.Client = nil

	t.Run("tools macros create", func(t *testing.T) {
		// fetch the number of macros prior to creation
		myInfo, err := testclient.MyInfo()
		if err != nil {
			panic(err)
		}
		priorMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			panic(err)
		}

		// create a new macro from the cli, in script mode
		args := strings.Split("-u admin --password changeme --insecure --script tools macros create -n testname -d testdesc -e testexpand", " ")
		errCode := tree.Execute(args)
		if errCode != 0 {
			t.Errorf("expected 0 exit code, got: %v", errCode)
		}

		// refetch macros to check the count has increased by one
		postMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			panic(err)
		}
		if len(postMacros) != len(priorMacros)+1 {
			t.Fatalf("expected post-create macros len (%v) == pre-create macros len+1 (%v)", len(postMacros), len(priorMacros))
		}
	})

	connection.End()
	connection.Client = nil

	t.Run("tools macros delete (dryrun)", func(t *testing.T) {
		// fetch the macros prior to deletion
		myInfo, err := testclient.MyInfo()
		if err != nil {
			panic(err)
		}
		priorMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			panic(err)
		}
		if len(priorMacros) < 1 {
			t.Skip("no macros to delete")
		}
		// pick a macro for deletion
		toDeleteID := priorMacros[0].ID
		t.Logf("Selecting macro %v (ID: %v) for faux-deletion", priorMacros[0].Name, priorMacros[0].ID)

		// create a new macro from the cli, in script mode
		args := strings.Split(
			fmt.Sprintf("-u admin --password changeme --insecure --script tools macros delete --dryrun --id %v",
				toDeleteID),
			" ")
		errCode := tree.Execute(args)
		if errCode != 0 {
			t.Errorf("expected 0 exit code, got: %v", errCode)
		}

		// refetch macros to check that count hasn't changed
		postMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			panic(err)
		}
		if len(postMacros) != len(priorMacros) {
			t.Fatalf("expected macro count to not change. post count: %v, pre count: %v",
				len(postMacros), len(priorMacros))
		}
		// ensure the selected macro still exists
		var found bool = false
		for _, m := range postMacros {
			if m.ID == toDeleteID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("Did not find ID %v in the post-faux-deletion list", toDeleteID)
		}
	})

	connection.End()
	connection.Client = nil

	t.Run("tools macros delete [failure: missing id]", func(t *testing.T) {
		//prepare IO
		stdoutData, stderrData, err := mockIO()
		if err != nil {
			restoreIO()
			panic(err)
		}

		// fetch the macros prior to deletion
		myInfo, err := testclient.MyInfo()
		if err != nil {
			panic(err)
		}
		priorMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			panic(err)
		}
		if len(priorMacros) < 1 {
			t.Skip("no macros to delete")
		}
		// pick a macro for deletion
		toDeleteID := priorMacros[0].ID
		t.Logf("Selecting macro %v (ID: %v) for faux-deletion", priorMacros[0].Name, priorMacros[0].ID)

		// create a new macro from the cli, in script mode
		args := strings.Split(
			"-u admin --password changeme --insecure --script tools macros delete",
			" ")
		errCode := tree.Execute(args)
		restoreIO()
		if errCode != 0 {
			t.Errorf("expected 0 exit code, got: %v", errCode)
		}

		results := <-stdoutData
		resultsErr := <-stderrData
		if resultsErr == "" {
			t.Error("empty stderr. Expected error message")
		}
		// check that no data was output to stdout in script and -o mode
		if results != "" {
			t.Errorf("non-empty stdout. Expected none. Got:\n(%v)\n", results)
		}

		// refetch macros to check that count hasn't changed
		postMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			panic(err)
		}
		if len(postMacros) != len(priorMacros) {
			t.Fatalf("expected macro count to not change. post count: %v, pre count: %v",
				len(postMacros), len(priorMacros))
		}
		// ensure the selected macro still exists
		var found bool = false
		for _, m := range postMacros {
			if m.ID == toDeleteID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("Did not find ID %v in the post-faux-deletion list", toDeleteID)
		}
	})

	connection.End()
	connection.Client = nil

	t.Run("tools macros delete", func(t *testing.T) {
		// fetch the macros prior to deletion
		myInfo, err := testclient.MyInfo()
		if err != nil {
			panic(err)
		}
		priorMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			panic(err)
		}
		if len(priorMacros) < 1 {
			t.Skip("no macros to delete")
		}
		// pick a macro for deletion
		toDeleteID := priorMacros[0].ID
		t.Logf("Selecting macro %v (ID: %v) for deletion", priorMacros[0].Name, priorMacros[0].ID)

		// create a new macro from the cli, in script mode
		args := strings.Split(fmt.Sprintf("-u admin --password changeme --insecure --script tools macros delete --id %v", toDeleteID), " ")
		errCode := tree.Execute(args)
		if errCode != 0 {
			t.Errorf("expected 0 exit code, got: %v", errCode)
		}

		// refetch macros to check the count has increased by one
		postMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			panic(err)
		}
		if len(postMacros) != len(priorMacros)-1 {
			t.Fatalf("expected post-delete macros len (%v) == pre-delete macros len-1 (%v)", len(postMacros), len(priorMacros))
		}
		// ensure the correct macro was deleted
		for _, m := range postMacros {
			if m.ID == toDeleteID {
				t.Log("ID of deletion attempt found still alive.")
				t.Log("priorMacros:\n")
				for _, prior := range priorMacros {
					t.Logf("%v (ID: %v)\n", prior.Name, prior.ID)
				}
				t.Log("postMacros:\n")
				for _, post := range postMacros {
					t.Logf("%v (ID: %v)\n", post.Name, post.ID)
				}
				t.FailNow()
			}
		}
	})

	connection.End()
	connection.Client = nil

	t.Run("query 'tags=gravwell'", func(t *testing.T) {
		//prepare IO
		stdoutData, stderrData, err := mockIO()
		if err != nil {
			restoreIO()
			panic(err)
		}

		// run the test body
		outfn := "testnoninteractive.query.json"
		qry := "query tag=gravwell"
		args := strings.Split("--insecure --script "+qry+
			" -o "+outfn+" --json", " ")

		errCode := tree.Execute(args)
		restoreIO()
		if errCode != 0 {
			t.Errorf("non-zero error code: %v", errCode)
		}

		<-stdoutData
		resultsErr := <-stderrData
		if resultsErr != "" {
			t.Errorf("non-empty stderr:\n(%v)", resultsErr)
		}

		// slurp the file, check for valid JSON
		output, err := os.ReadFile(outfn)
		t.Logf("slurping %v...", outfn)
		if err != nil {
			t.Fatal(err)
		} else if strings.TrimSpace(string(output)) == "" {
			t.Fatal("empty output file")
		}
		// we cannot check json validity because the grav client lib outputs individual JSON
		// records, not a single blob
		/*if !json.Valid(output) {
			t.Errorf("json is not valid")
		}*/
		// fetch the search and check that record counts line up
		searches, err := testclient.GetSearchHistoryRange(0, 5)
		if err != nil {
			t.Fatal(err)
		} else if len(searches) < 1 {
			t.Fatalf("found no previous searches")
		}
		//var search types.SearchLog
		for _, s := range searches {
			if s.UserQuery == qry {
				//search = s
				// get SearchHistory* does not pull back the searchID, meaning I
				// cannnot pull more details about the search
				// TODO
				break
			}
		}

		// clean up
		if !t.Failed() {
			os.Remove(outfn)
		}
	})

	connection.End()
	connection.Client = nil

	/*t.Run("query reference ID", func(t *testing.T) {
		// fetch a scheduled query to run
		ssl, err := testclient.GetSearchHistory()
		if err != nil {
			panic(err)
		} else if len(ssl) < 1 {
			t.Skip("no existing scheduled searches to test against")
		}
		ssguid := ssl[rand.Intn(len(ssl))].UID
		ssqry := ssl[rand.Intn(len(ssl))].UserQuery

		// prepare IO
		stdoutData, stderrData, err := mockIO()
		if err != nil {
			restoreIO()
			panic(err)
		}

		// run the test body
		outfn := "testnoninteractive.query_reference_ID.csv"
		args := strings.Split(
			fmt.Sprintf("--insecure --script query -r %v -o %v --csv", ssguid, outfn),
			" ")

		errCode := tree.Execute(args)
		restoreIO()
		if errCode != 0 {
			t.Errorf("non-zero error code: %v", errCode)
		}

		// fetch outputs
		<-stdoutData
		resultsErr := <-stderrData
		if resultsErr != "" {
			t.Errorf("non-empty stderr:\n(%v)", resultsErr)
		}

		// check in search history for our expected search
		prevSearchNumToCheck := 7
		searches, err := testclient.GetSearchHistoryRange(0, prevSearchNumToCheck)
		if err != nil {
			panic(err)
		} else if len(searches) < 1 {
			t.Fatalf("found no previous searches")
		}
		var found bool
		for _, s := range searches {
			if s.UserQuery == ssqry {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("Failed to find executed query (%v) in past %v searches",
				ssqry, prevSearchNumToCheck)
		}

		// clean up
		if !t.Failed() {
			os.Remove(outfn)
		}
	})*/

}

func TestNonInteractiveQueryFileOut(t *testing.T) {
	// create results to ensure data is returned
	testclient, err := grav.NewOpts(grav.Opts{Server: server, UseHttps: false, InsecureNoEnforceCerts: true})
	if err != nil {
		panic(err)
	}
	if err = testclient.Login(user, password); err != nil {
		panic(err)
	}

	if s, err := testclient.StartSearch("tag=gravwell",
		time.Now().Add(-1*time.Second), time.Now(), false); err != nil {
		t.Skip("Failed to create search as base data: ", err)
	} else {
		if err := testclient.WaitForSearch(s); err != nil {
			t.Skip("Failed to wait for base data search: ", err)
		}
	}

	dir := t.TempDir()
	tempFilePrefix := "gwcliTestNonInteractiveQueryOut"

	t.Run("raw", func(t *testing.T) {
		var outfn string = path.Join(dir, fmt.Sprintf("%v%d", tempFilePrefix, rand.Uint32()))

		qry := "query tag=gravwell"
		args := strings.Split("--insecure --script "+qry+" -o "+outfn, " ")
		t.Log("Args: ", args)

		exitCode := tree.Execute(args)
		nonZeroExit(t, exitCode)

		// check the file has data
		invalidSize(t, outfn)
	})

	t.Run("raw append", func(t *testing.T) {
		var outfn string = path.Join(dir, fmt.Sprintf("%v%d", tempFilePrefix, rand.Uint32()))

		baseData := "Hello World"

		// prepopulate the file with data to check for append
		if err := os.WriteFile(outfn, []byte(baseData+"\n"), 0644); err != nil {
			t.Fatalf("Failed to prepopulate %v: %v", outfn, err)
		}

		priorFI, err := os.Stat(outfn)
		if err != nil {
			t.Fatal(err)
		}
		priorSize := priorFI.Size()
		if priorSize <= 0 {
			t.Fatalf("test file to append to has invalid size: %v", priorSize)
		}

		// execute the query in append mode
		qry := "query tag=gravwell"
		args := strings.Split("--insecure --script "+qry+" -o "+outfn+" --append", " ")
		t.Log("Args: ", args)
		exitCode := tree.Execute(args)
		nonZeroExit(t, exitCode)

		// check the file has more data than before
		postFI, err := os.Stat(outfn)
		if err != nil {
			t.Fatal(err)
		}
		if postFI.Size() <= priorSize {
			t.Fatalf("expected post size (%v) to be greater than prior size (%v)", postFI.Size(), priorSize)
		}

		// check that the first line still exists
		f, err := os.Open(outfn)
		if err != nil {
			t.Fatalf("failed to read from file %v: %v", outfn, err)
		}
		defer f.Close()
		scan := bufio.NewScanner(f)
		if !scan.Scan() {
			t.Fatal("failed to scan first line. Error? ", scan.Err())
		}
		firstLine := scan.Text()
		if firstLine != baseData {
			t.Fatalf("expected first line of file to be %v, got %v", baseData, firstLine)
		}
	})

	t.Run("json", func(t *testing.T) {
		var outfn string = path.Join(dir, fmt.Sprintf("%v%d", tempFilePrefix, rand.Uint32()))
		// execute the query in append mode
		qry := "query tag=gravwell"
		args := strings.Split("--insecure --script "+qry+" -o "+outfn+" --json", " ")
		t.Log("Args: ", args)
		exitCode := tree.Execute(args)
		nonZeroExit(t, exitCode)

		// check the file has data
		invalidSize(t, outfn)

		// check each record is valid JSON
		f, err := os.Open(outfn)
		if err != nil {
			t.Fatalf(openFileFailF, f.Name())
		}
		scan := bufio.NewScanner(f)
		for scan.Scan() {
			line := strings.TrimSpace(scan.Text())
			if line == "" {
				continue
			}
			if !json.Valid([]byte(line)) {
				t.Fatalf("record %v is not valid JSON", line)
			}
		}
	})

	t.Run("csv", func(t *testing.T) {
		var outfn string = path.Join(dir, fmt.Sprintf("%v%d", tempFilePrefix, rand.Uint32()))
		// execute the query in append mode
		qry := "query tag=gravwell"
		args := strings.Split("--insecure --script "+qry+" -o "+outfn+" --csv", " ")
		t.Log("Args: ", args)
		exitCode := tree.Execute(args)
		nonZeroExit(t, exitCode)

		// check the file has data
		invalidSize(t, outfn)

		// check each record is valid JSON
		f, err := os.Open(outfn)
		if err != nil {
			t.Fatalf(openFileFailF, f.Name())
		}
		// csv package does not have a .Valid() like JSON
		// instead, just check that we are able to read the data

		s := csv.NewReader(f)
		s.ReuseRecord = true // don't care about actual data; reduce allocations
		for {
			if r, err := s.Read(); err != nil {
				if err == io.EOF {
					break
				} else {
					t.Fatalf("bad csv record '%v': %v", r, err)
				}
			}

		}
	})
}

//#endregion

func mockIO() (stdoutData chan string, stderrData chan string, err error) {
	// capture stdout
	var readMockStdout *os.File
	readMockStdout, mockStdout, err = os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	stdoutData = make(chan string) // pass data from read to write
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, readMockStdout)
		stdoutData <- buf.String()
	}()
	os.Stdout = mockStdout

	// capture stderr
	var readMockStderr *os.File
	readMockStderr, mockStderr, err = os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	stderrData = make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, readMockStderr)
		stderrData <- buf.String()
	}()
	os.Stderr = mockStderr

	return stdoutData, stderrData, nil
}

func restoreIO() {
	// stdout
	if mockStdout != nil {
		_ = mockStdout.Close()
	}
	if realStdout == nil {
		panic("failed to restore stdout; no saved handle")
	}
	os.Stdout = realStdout

	// stderr
	if mockStderr != nil {
		_ = mockStderr.Close()
	}
	if realStderr == nil {
		panic("failed to restore stderr; no saved handle")
	}
	os.Stderr = realStderr
}

// #region strings and failure checks

// Dies if code is <> 0
func nonZeroExit(t *testing.T, code int) {
	t.Helper()
	if code != 0 {
		t.Fatalf("non-zero exit code %v", code)
	}

}

func invalidSize(t *testing.T, fn string) {
	t.Helper()
	fi, err := os.Stat(fn)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() <= 0 {
		t.Fatal(fi.Name(), " has invalid size: %v", fi.Size())
	}
}

const (
	openFileFailF = "failed to open file %v"
)

// #endregion
