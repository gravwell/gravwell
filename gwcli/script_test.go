//go:build !ci
// +build !ci

/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

/*
This file covers tests for using gwcli in --script mode (from a user's shell or via an external script).

These tests make destructive changes to the gravwell server; make sure you are targeting a safe, clean server!

Each test is intended to be self-contained but, due to gwcli's usage of singletons,
do not account for parallelism at a test level
(testing in multiple processes, not goroutines, is acceptable).
*/

import (
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
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/tree"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/utils/weave"
)

const ( // testing server credentials
	user     = "admin"
	password = "changeme"
	server   = "localhost:80"
)

var realStderr, mockStderr, realStdout, mockStdout *os.File

// Only prints the given string if verbose mode is enabled.
func verboseln(s string) {
	if testing.Verbose() {
		fmt.Println(s)
	}
}

func init() {
	// ensure we capture the normal STDOUT and STDERR so we can restore to them
	realStderr = os.Stderr
	realStdout = os.Stdout
}

// Tests the 'macro' action of gwcli
func TestMacros(t *testing.T) {
	verboseln("testing macros...")
	// connect to the server for manual calls
	testclient, err := grav.NewOpts(grav.Opts{Server: server, UseHttps: false, InsecureNoEnforceCerts: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = testclient.Login(user, password); err != nil {
		t.Fatal(err)
	}

	t.Run("macros list --csv", func(t *testing.T) {
		verboseln("\tmacros: list in CSV form")
		// generate results manually, for comparison
		myInfo, err := testclient.MyInfo()
		if err != nil {
			t.Fatal(err)
		}
		// get the current list of macros so we can validate that gwcli turned back the same ones
		macros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			t.Fatal(err)
		}
		columns := []string{"UID", "Global", "Name"}
		want := strings.TrimSpace(weave.ToCSV(macros, columns))
		if want == "" { // empty list command results output "no data found"
			want = "no data found"
		}

		// run the test body
		cmd := fmt.Sprintf("-u %s -p %s --insecure --script macros list --csv --columns=%s", user, password, strings.Join(columns, ","))
		statusCode, stdout, stderr := executeCmd(t, cmd)

		// check the outcome
		nonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "", stderr)
		checkResult(t, true, "stdout", want, strings.TrimSpace(stdout))
	})

	t.Run("macros create", func(t *testing.T) {
		var (
			macroName = randomdata.SillyName()
			macroDesc = "macro created for automated testing"
			macroExp  = "testexpand"
		)
		verboseln("\tmacros: create  $" + macroName + " --> %" + macroExp)
		// fetch the number of macros prior to creation
		myInfo, err := testclient.MyInfo()
		if err != nil {
			panic(err)
		}
		priorMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			panic(err)
		}

		// ensure the macro DNE, reroll it if it does
		for {
			if slices.ContainsFunc(priorMacros, func(sm types.SearchMacro) bool {
				return macroName == sm.Name
			}) {
				//reroll name
				macroName = randomdata.SillyName()
				continue
			}
			break
		}

		// create a new macro from the cli, in script mode
		cmd := fmt.Sprintf("-u %s --password %s --insecure --script macros create -n %s -d %s -e %s", user, password, macroName, macroDesc, macroExp)
		statusCode, _, stderr := executeCmd(t, cmd)
		nonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "", stderr)
		// refetch macros to check the count has increased by one
		postMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			panic(err)
		}
		if len(postMacros) != len(priorMacros)+1 {
			t.Fatalf("expected post-create macros len(%v) == pre-create macros len(%v)+1 ", len(postMacros), len(priorMacros))
		}
		// TODO parse out macro ID from stdout and ensure it exists in the postMacros list
	})

	t.Run("macros list --json", func(t *testing.T) {
		verboseln("\tmacros: list in JSON form after creating a macro")
		// generate results manually, for comparison
		myInfo, err := testclient.MyInfo()
		if err != nil {
			t.Fatal(err)
		}
		// get the current list of macros so we can validate that gwcli turned back the same ones
		macros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			t.Fatal(err)
		}
		columns := []string{"UID", "Global", "Name", "WriteAccess.GIDs", "Description", "Expansion", "Labels"}
		var want string
		if json, err := weave.ToJSON(macros, columns); err != nil {
			t.Fatal(err)
		} else {
			want = strings.TrimSpace(json)
			if want == "" { // empty list command outputs "no data found"
				want = "no data found"
			}
		}

		cmd := fmt.Sprintf("-u %s -p %s --insecure --script macros list --json --columns=%s", user, password, strings.Join(columns, ","))
		statusCode, stdout, stderr := executeCmd(t, cmd)

		// check the outcome
		nonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "", stderr)
		checkResult(t, true, "stdout", want, strings.TrimSpace(stdout))
	})

	t.Run("macros delete (dryrun)", func(t *testing.T) {
		verboseln("\tmacros: dryrun delete a random macro to confirm it isn't actually deleted")
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
		// pick a macro for faux-deletion
		toDeleteID := priorMacros[0].ID
		t.Logf("Selecting macro %v (ID: %v) for faux-deletion", priorMacros[0].Name, priorMacros[0].ID)

		cmd := fmt.Sprintf("-u %s -p %s --insecure --script macros delete --dryrun --id=%d", user, password, toDeleteID)
		statusCode, _, stderr := executeCmd(t, cmd)

		// check the outcome
		nonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "", stderr)

		// refetch macros to check that count hasn't changed
		postMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			t.Fatal(err)
		} else if len(postMacros) != len(priorMacros) {
			t.Fatalf("expected macro count to not change. post count: %v, pre count: %v",
				len(postMacros), len(priorMacros))
		}
		// ensure the selected macro still exists
		var found = false
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
		verboseln("\tmacros: submit invalid delete command (missing id)")

		// fetch the macros prior to deletion
		myInfo, err := testclient.MyInfo()
		if err != nil {
			panic(err)
		}
		priorMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			t.Fatal(err)
		}
		if len(priorMacros) < 1 {
			t.Skip("no macros to delete")
		}

		cmd := fmt.Sprintf("-u %s -p %s --insecure --script macros delete", user, password)
		statusCode, stdout, stderr := executeCmd(t, cmd)

		// check the outcome
		nonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stdout", "", stdout)

		// refetch macros to check that count hasn't changed
		postMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			t.Fatal(err)
		}
		if len(postMacros) != len(priorMacros) {
			t.Fatalf("expected macro count to not change. post count: %v, pre count: %v",
				len(postMacros), len(priorMacros))
		}
	})

	t.Run("macros delete", func(t *testing.T) {
		verboseln("\tmacros: submit valid delete command")
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

		cmd := fmt.Sprintf("-u %s -p %s --insecure --script macros delete --id %v", user, password, toDeleteID)
		statusCode, _, stderr := executeCmd(t, cmd)

		// check the outcome
		nonZeroExit(t, statusCode, stderr)

		// refetch macros to check the count has decreased by one
		postMacros, err := testclient.GetUserMacros(myInfo.UID)
		if err != nil {
			t.Fatal(err)
		} else if len(postMacros) != len(priorMacros)-1 {
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

}

func TestQueries(t *testing.T) {
	verboseln("testing queries...")

	// connect to the server for manual calls
	testclient, err := grav.NewOpts(grav.Opts{Server: server, UseHttps: false, InsecureNoEnforceCerts: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = testclient.Login(user, password); err != nil {
		t.Fatal(err)
	}

	t.Run("query output json to file", func(t *testing.T) {
		verboseln("\tqueries: querying for data as json and outputting it to a file in foreground")
		outPath := path.Join(t.TempDir(), "out.json")
		qry := "tag=gravwell"

		// TODO need to make sure -o is valid before submitting the query
		cmd := fmt.Sprintf("-u %s -p %s --insecure --script query %s -o %s --json", user, password, qry, outPath)
		statusCode, stdout, stderr := executeCmd(t, cmd)
		nonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "", stderr)

		// check that the search was as we expected
		sid := skimSID(t, stdout)
		if sid == "" {
			t.Fatal("failed to scan search ID out of stdout")
		}
		t.Logf("scanned out sid %s", sid)
		// fetch the search
		si, err := testclient.SearchInfo(sid)
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

		// match item count against actual output
		if si.ItemCount == 0 {
			// the file should not exist
			_, err := os.Stat(outPath)
			if err == nil || !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("no results returned, but %s exists (or an error occurred). Error: %v", outPath, err)
			}
		} else {
			// slurp the file
			output, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatalf("failed to slurp file %s: %v", outPath, err)
			} else if strings.TrimSpace(string(output)) == "" {
				t.Fatalf("%s is empty, but the search turned back %d records", outPath, si.ItemCount)
			}
			// check that each record is valid JSON
			var count uint
			for record := range bytes.SplitSeq(output, []byte{'\n'}) {
				if strings.TrimSpace(string(record)) == "" {
					continue
				}
				count += 1
				if !json.Valid(record) && string(record) != "[]" { // Go does not consider '[]' valid JSON, but we do
					t.Errorf("'%v' is not valid JSON", record)
				}
			}
			// check the record count matches the search's item count
			if count != uint(si.ItemCount) {
				t.Fatalf("incorrect item count in file: %s", expectedActual(si.ItemCount, count))
			}
		}
	})

	t.Run("background query 'tags=gravwell limit 3'", func(t *testing.T) {
		verboseln("\tqueries: submitting a background query with ignored --output flag")
		outPath := path.Join(t.TempDir(), "IShouldNotBeCreated.txt")
		qry := "tag=gravwell"

		cmd := fmt.Sprintf("-u %s -p %s --insecure --script query %s -o %s --background", user, password, qry, outPath)
		statusCode, stdout, stderr := executeCmd(t, cmd)
		nonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "WARN: ignoring flag --output due to --background", strings.TrimSpace(stderr))

		// ensure the file was *not* created
		if _, err := os.Stat(outPath); err == nil || !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("an output file (%v) was created, but should not have been", outPath)
		}

		// ensure that we were warned about using -o

		// parse out the sid
		sid := skimSID(t, stdout)
		if sid == "" {
			t.Fatal("failed to scan search ID out of stdout")
		}
		t.Logf("scanned out sid %s", sid)
		// fetch the search
		si, err := testclient.SearchInfo(sid)
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

	t.Run("query output append to file", func(t *testing.T) {
		verboseln("\tqueries: submitting a query and appending results to an existing file")

		var outPath = path.Join(t.TempDir(), "append.out")
		// populate the file with some garbage data
		var baseData strings.Builder
		if _, err := baseData.WriteString("Hello World"); err != nil {
			t.Fatal(err)
		}
		for range 10 {
			if _, err := baseData.WriteString(strconv.FormatInt(rand.Int63(), 10)); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(outPath, []byte(baseData.String()+"\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// get information about the prior state of the file
		priorFI, err := os.Stat(outPath)
		if err != nil {
			t.Fatal(err)
		} else if priorFI.Size() <= 0 {
			t.Fatalf("test file to append to has invalid size: %v", priorFI.Size())
		}

		// execute the query in append mode
		qry := "tag=gravwell limit 1"
		cmd := fmt.Sprintf("-u %s -p %s --insecure --script query %s -o %s --append", user, password, qry, outPath)
		statusCode, _, stderr := executeCmd(t, cmd)
		nonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "", stderr)

		// check the file has more data than before
		postFI, err := os.Stat(outPath)
		if err != nil {
			t.Fatal(err)
		}
		if postFI.Size() <= priorFI.Size() {
			t.Fatalf("expected post size (%v) to be greater than prior size (%v)", postFI.Size(), priorFI.Size())
		}

		// check that the initial data still exists
		f, err := os.Open(outPath)
		if err != nil {
			t.Fatalf("failed to read from file %v: %v", outPath, err)
		}
		defer f.Close()
		fileDataB, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		fileData := string(fileDataB)
		if !strings.HasPrefix(fileData, baseData.String()) {
			t.Fatalf("base data is absent from appended file. Expected to find the following file prefix:\n%v\nFinal file: %v\n", baseData.String(), fileData)
		}
	})

	t.Run("query csv", func(t *testing.T) {
		verboseln("\tqueries: query output csv to stdout")

		qry := "tag=gravwell limit 1"
		cmd := fmt.Sprintf("-u %s -p %s --insecure --script query %s --csv", user, password, qry)
		statusCode, stdout, stderr := executeCmd(t, cmd)
		nonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "", stderr)

		// csv package does not have a .Valid() like JSON
		// instead, just check that we are able to read the data

		rdr := strings.NewReader(stdout)

		s := csv.NewReader(rdr)
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

	t.Run("attach to backgrounded, stdout", func(t *testing.T) {
		verboseln("\tqueries: submit background query with long delay, reattach and wait for it")

		// ensure they are running against a debug build (so we can use the sleep module)
		// TODO

		var sid string
		{ // submit a background query
			bgQry := "tag=gravwell | sleep 5s"
			// parse the query, as this will tell us early if sleep is not available (aka we are not in a debug build)
			if err := testclient.ParseSearch(bgQry); err != nil {
				t.Skip("background query could be not parsed: ", err)
			}

			cmd := fmt.Sprintf("-u %s -p %s --insecure --script query %s", user, password, bgQry)
			statusCode, stdout, stderr := executeCmd(t, cmd)
			nonZeroExit(t, statusCode, stderr)
			checkResult(t, false, "stderr", "", stderr)

			// save off background query sid
			sid = skimSID(t, stdout)
			if sid == "" {
				t.Fatal("failed to scan search ID out of stdout")
			}
			t.Logf("scanned out sid %s", sid)
		}

		// attach to background query
		cmd := fmt.Sprintf("-u %s -p %s --insecure --script attach %s --csv", user, password, sid)
		statusCode, attachSTDOUT, stderr := executeCmd(t, cmd)
		nonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "", stderr)

		// fetch the background query's results manually
		var actualOut string
		{
			var sb strings.Builder
			rc, err := testclient.DownloadSearch(sid, types.TimeRange{}, "text")
			if err != nil {
				t.Fatal("failed to manually fetch query results: ", err)
			}
			if _, err := io.Copy(&sb, rc); err != nil {
				// TODO may need to know written to know if it should be empty
				t.Fatal(err)
			}
			actualOut = sb.String()
		}
		// check stdout
		// don't really care about the data, just that it matches what it should
		if attachSTDOUT != actualOut {
			t.Fatalf("attach pulled back different results from query (sid=%v).%v", sid, expectedActual(actualOut, attachSTDOUT))
		}
	})

	// TODO attach to backgrounded, output to file

	// TODO attach to backgrounded after its completion

	// TODO attach to foreground after its completion

	// TODO attach to foreground before its completion
}

//#endregion

//#region helper functions

// Mocks STDOUT and STDERR with new pipes so the tests can intercept data from them.
// Returns the channels from which to get their data.
// Dies and reverts changes if any of the pipes fail.
func mockIO(t *testing.T) (stdoutData chan string, stderrData chan string) {
	defer func() {
		// if an error occurred, restore standard IO
		if t.Failed() {
			restoreIO()
		}
	}()
	var err error
	// capture stdout
	var readMockStdout *os.File
	readMockStdout, mockStdout, err = os.Pipe()
	if err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}
	stderrData = make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, readMockStderr)
		stderrData <- buf.String()
	}()
	os.Stderr = mockStderr

	return stdoutData, stderrData
}

// Closes the mocked STDOUT and STDERR pipes and returns them to the "real" variants (the default state of os.Stdout and os.Stderr) when the test began.
// Sister function to mockIO().
func restoreIO() {
	// stdout
	if mockStdout != nil {
		_ = mockStdout.Close()
		mockStdout = nil
	}
	if realStdout == nil {
		panic("failed to restore stdout; no saved handle")
	}
	os.Stdout = realStdout

	// stderr
	if mockStderr != nil {
		_ = mockStderr.Close()
		mockStderr = nil
	}
	if realStderr == nil {
		panic("failed to restore stderr; no saved handle")
	}
	os.Stderr = realStderr
}

// Runs the given command, returning the final status code and the values the command spit into STDERR and STDOUT.
// The command is run against the command tree, which implies client creation and authentication.
// Registers a t.Cleanup to close and nil the client.
//
// Logs the command run in case the test fails.
//
// Roughly similar to exec.Command(<cmd>).Output()
//
// Returns the status code of the command and the data contained in stdout and stderr.
func executeCmd(t *testing.T, cmd string) (statusCode int, stdoutData, stderrData string) {
	t.Helper()

	// prepare IO
	outch, errch := mockIO(t)

	t.Log(cmd)
	errCode := tree.Execute(strings.Split(cmd, " "))
	t.Cleanup(func() { // when we are done testing, destroy the client
		connection.End()
		connection.Client = nil
	})
	restoreIO()

	// fetch output
	results := <-outch
	resultsErr := <-errch

	return errCode, results, resultsErr

}

//#endregion helper functions

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
func nonZeroExit(t *testing.T, code int, stderr string) {
	t.Helper()
	if code != 0 {
		t.Fatalf("non-zero exit code %v.\nstderr: '%v'", code, stderr)
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

// Fails if expected != actual.
// source is probably "stderr" or "stdout".
// If fatal, test execution will stop.
func checkResult(t *testing.T, fatal bool, source, expected, actual string) {
	t.Helper()

	if expected != actual {
		if fatal {
			t.Fatalf("bad %s: %s", source, expectedActual(expected, actual))
		} else {
			t.Errorf("bad %s: %s", source, expectedActual(expected, actual))
		}
	}
}

// Returns a string declaring what was expected and what we got instead.
// NOTE(rlandau): Prefixes the string with a newline.
func expectedActual(expected, actual any) string {
	return fmt.Sprintf("\n\tExpected:'%v'\n\tGot:'%v'", expected, actual)
}

const (
	openFileFailF = "failed to open file %v"
)

// #endregion
