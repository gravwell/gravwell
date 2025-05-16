/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Tests from a complete-program perspective, confirming consistent input begets
// reliable output.
// Expects that the constant server string points to a development server with default credentials.

package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/tree"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/utils/weave"
)

const ( // mock credentials
	user     = "admin"
	password = "changeme"
	server   = "localhost:80"
)

var realStderr, mockStderr, realStdout, mockStdout *os.File

//#region non-interactive

// Runs a variety of tests as a user of gwcli, from the scriptable interface.
// All tests have their STDERR and STDOUT captured for evaluation.
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

	t.Run("macros list --csv", func(t *testing.T) {
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
		want := strings.TrimSpace(weave.ToCSV(macros, columns))
		if want == "" { // empty list command results output "no data found"
			want = "no data found"
		}

		// prepare IO
		stdoutData, stderrData, err := mockIO()
		if err != nil {
			restoreIO()
			panic(err)
		}

		args := strings.Split("-u admin -p changeme --insecure --script macros list --csv --columns=UID,Global,Name", " ")

		// run the test body
		errCode := tree.Execute(args)
		// need to reset the client used by gwcli between runs
		connection.End()
		connection.Client = nil
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
			t.Fatalf("output mismatch\nwant:\n(%v)\ngot:\n(%v)\n", want, results)
		}
	})

	t.Run("macros create", func(t *testing.T) {
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
		args := strings.Split("-u admin --password changeme --insecure --script macros create -n testname -d testdesc -e testexpand", " ")
		errCode := tree.Execute(args)
		t.Cleanup(func() {
			connection.End()
			connection.Client = nil
		})
		if errCode != 0 {
			t.Errorf("expected 0 exit code, got: %v", errCode)
		}

		// refetch macros to check the count has increased by one
		postMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			panic(err)
		}
		if len(postMacros) != len(priorMacros)+1 {
			t.Fatalf("expected post-create macros len(%v) == pre-create macros len(%v)+1 ", len(postMacros), len(priorMacros))
		}
	})

	t.Run("macros delete (dryrun)", func(t *testing.T) {
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
			fmt.Sprintf("-u admin --password changeme --insecure --script macros delete --dryrun --id %v",
				toDeleteID),
			" ")
		errCode := tree.Execute(args)
		t.Cleanup(func() {
			connection.End()
			connection.Client = nil
		})
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

	t.Run("macros delete [failure: missing id]", func(t *testing.T) {
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
			"-u admin --password changeme --insecure --script macros delete",
			" ")
		errCode := tree.Execute(args)
		t.Cleanup(func() {
			connection.End()
			connection.Client = nil
		})
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

	t.Run("macros delete", func(t *testing.T) {
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
		args := strings.Split(fmt.Sprintf("-u admin --password changeme --insecure --script macros delete --id %v", toDeleteID), " ")
		errCode := tree.Execute(args)
		t.Cleanup(func() {
			connection.End()
			connection.Client = nil
		})
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

	t.Run("query 'tags=gravwell'", func(t *testing.T) {
		//prepare IO
		stdoutData, stderrData, err := mockIO()
		if err != nil {
			restoreIO()
			panic(err)
		}

		// run the test body
		outfn := "testnoninteractive.query.json"
		qry := "tag=gravwell"
		args := strings.Split("--insecure --script query "+qry+
			" -o "+outfn+" --json", " ")

		errCode := tree.Execute(args)
		t.Cleanup(func() {
			connection.End()
			connection.Client = nil
		})
		restoreIO()
		if errCode != 0 {
			t.Errorf("non-zero error code: %v", errCode)
		}

		resultOut := <-stdoutData
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

		sid := skimSID(t, resultOut)
		if sid == "" {
			t.Fatal("failed to scan search ID out of stdout")
		}
		t.Log("scanned out sid ", sid)
		// fetch the search
		si, err := testclient.SearchInfo(fmt.Sprintf("%s", sid))
		if err != nil {
			t.Fatalf("failed to get information on search %s", sid)
		}
		if si.Background {
			t.Errorf("search was backgrounded")
		}
		if si.UserQuery != qry {
			t.Errorf("searchID %s turned back a different query.\nExpected:%v\nGot:%v", sid, qry, si.UserQuery)
		}
		if si.Error != "" {
			t.Errorf("searchID %s turned back an error: %v", sid, si.Error)
		}

		// clean up
		if !t.Failed() {
			os.Remove(outfn)
		}
	})

	t.Run("background query 'tags=gravwell limit 3'", func(t *testing.T) {
		//prepare IO
		stdoutData, stderrData, err := mockIO()
		if err != nil {
			restoreIO()
			panic(err)
		}

		// run the test body
		outfn := "IShouldNotBeCreated.txt"
		t.Cleanup(func() { os.Remove(outfn) }) // this should be ineffectual
		qry := "tag=gravwell"
		args := strings.Split("--insecure --script query "+qry+
			" -o "+outfn+" --background", " ")

		errCode := tree.Execute(args)
		t.Cleanup(func() {
			connection.End()
			connection.Client = nil
		})
		restoreIO()
		if errCode != 0 {
			t.Errorf("non-zero error code: %v", errCode)
		}

		// ensure the file was *not* created
		if _, err := os.Stat(outfn); err == nil || !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("an output file (%v) was created, but should not have been", outfn)
		}

		resultsOut := strings.TrimSpace(<-stdoutData)
		resultsErr := strings.TrimSpace(<-stderrData)

		// ensure that we were warned about using -o
		expectedWarning := "WARN: ignoring flag --output due to --background"
		if !strings.EqualFold(resultsErr, expectedWarning) {
			t.Errorf("stderr is not as expected.\nExpected:%v\nGot:%v", expectedWarning, resultsErr)
		}

		// parse out the sid
		sid := skimSID(t, resultsOut)
		if sid == "" {
			t.Fatal("failed to scan search ID out of stdout")
		}
		t.Log("scanned out sid ", sid)
		// fetch the search
		si, err := testclient.SearchInfo(fmt.Sprintf("%s", sid))
		if err != nil {
			t.Fatalf("failed to get information on search %s", sid)
		}
		if !si.Background {
			t.Errorf("search was not backgrounded")
		}
		if si.UserQuery != qry {
			t.Errorf("searchID %s turned back a different query.\nExpected:%v\nGot:%v", sid, qry, si.UserQuery)
		}
		if si.Error != "" {
			t.Errorf("searchID %s turned back an error: %v", sid, si.Error)
		}
	})

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

// Mocks STDOUT and STDERR with new pipes so the tests can intercept data from them.
// Returns the channels from which to get their data.
// sister function to restoreIO()
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

// Closes the mocked STDOUT and STDERR pipes and returns them to the "real" variants (the default state of os.Stdout and os.Stderr) when the test began.
// Sister function to mockIO().
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

var sidRGX = regexp.MustCompile(`query \(ID: (\d+)\)`)

// Given the standard output, it scans out the search ID from the 'query successful' strings.
// Returns the first matching instance.
// If no matching messages are found, returns the empty string.
func skimSID(t *testing.T, stdout string) (sid string) {
	t.Helper()
	if stdout == "" {
		t.Log("cannot search for SID in empty data")
		return ""
	}
	resultsOut := strings.SplitSeq(stdout, "\n")
	// check each entry in resultsOut until we find the correct string or run out of entries
	/*var (
		fgbg    string // unused
		numeric uint64
	)*/
	for res := range resultsOut {
		t.Logf("scanning line '%s'", res)

		match := sidRGX.FindStringSubmatch(res)
		if match != nil {
			return match[1] // want the first capture group
		}

		/*
			matches := sidRGX.FindStringSubmatch(res)
			sidRGX.SubexpNames()
			t.Log(matches)
			if len(matches) == 1 {
				return matches[0]
			}*/

		/*if n, err := fmt.Sscanf(res, "Successfully %s query (ID: %d)", &fgbg, &numeric); err != nil {
			if n == 2 && sid != "" {
				// chomp the ')'
				return strconv.FormatUint(numeric, 10)
			}
		}*/
	}

	return ""
}

// #region strings and failure checks

// Dies if code is <> 0
func nonZeroExit(t *testing.T, code int) {
	t.Helper()
	if code != 0 {
		t.Fatalf("non-zero exit code %v", code)
	}

}

// Dies if the file associated to the given path DNE or is empty.
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
