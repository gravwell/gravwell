//go:build noci && integration

/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package main_test provides integrations tests for gwcli, executing it as a standalone binary.
// These tests requires the user to provide a path to the gwcli binary to be tested via -binary.
// These tests also require a gravwell instance to target; the instance will be destructively altered.
// This defaults to localhost:80, but can be specified via -server.
//
// Unless otherwise stated, these tests are executed in no-interactive mode (-x).
package main_test

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pallinder/go-randomdata"
	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type authMethod uint

const (
	noAuth    authMethod = iota // do not provide any login
	admin_u_p                   // login with admin username and password
	api                         // login with the api token

	second_u_p // login with the second user's u/p
)

// All of these are set by Main.
var (
	serverString string
	tCfgDir      string // path to the temporary config dir
	binaryPath   string
	argMetaBase  []string // basic arguments passed to every command (server, no-interactive, insecure)
	argAPI       string   // --api argument
	client       *grav.Client

	// second user
	secondUser string
	secondPass string
)

func init() {
	flag.StringVar(&binaryPath, "binary", "", "REQUIRED. Path to the gwcli binary")
}

func TestMain(m *testing.M) {
	flag.Parse()
	if binaryPath == "" {
		fmt.Fprintln(os.Stderr, "you must set -binary")
		os.Exit(1)
	}
	if _, err := exec.LookPath(binaryPath); err != nil && !errors.Is(err, exec.ErrDot) {
		fmt.Fprintf(os.Stderr, "binary existence check failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("testing against binary", binaryPath)

	serverString = testsupport.Server()
	fmt.Println("connecting to test server @", serverString)

	// create an admin client to test data against
	var err error
	client, err = grav.New(serverString, false, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate a standalone client for server '%v': %v\n", serverString, err)
		os.Exit(1)
	}
	if err := client.Login("admin", "changeme"); err != nil {
		fmt.Fprintf(os.Stderr, "failed to login standalone client with default admin credentials: %v\n", err)
		os.Exit(1)
	}
	// generate an API token with all capabilities we can provide to our tests
	caps, err := client.TokenCapabilities()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to retrieve capabilities list: %v\n", err)
		os.Exit(1)
	}
	tkn, err := client.CreateToken(types.Token{
		CommonFields: types.CommonFields{
			Name:        "integration_test_login_token",
			Description: "grants all capabilities",
		},
		ExpiresAt:    time.Now().Add(time.Hour),
		Capabilities: caps,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate full admin token: %v\n", err)
		os.Exit(1)
	}

	// create a second user for testing non-admin things
	secondUser = randomdata.Letters(6)
	secondPass = randomdata.Digits(4)
	second, err := client.CreateUser(types.AddUser{
		Username: secondUser,
		Password: secondPass,
		Name:     randomdata.FullName(randomdata.RandomGender),
		Email:    randomdata.Email(),
		Admin:    false,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create second user: %v\n", err)
		os.Exit(1)
	}

	// compose meta args
	argMetaBase = []string{"--server=" + serverString,
		"--insecure",
		"--loglevel=debug",
		"-x",
	}
	argAPI = "--api=" + tkn.Value

	// set up the configuration directory
	tCfgDir, err = os.MkdirTemp("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate temporary config directory: %v\n", err)
		os.Exit(1)
	}

	ec := m.Run()

	// clean up after ourselves, at least a little
	if err := client.DeleteUser(second.ID); err != nil {
		fmt.Fprintln(os.Stderr, "failed to clean up second user: ", err)
	}
	if err := client.DeleteToken(tkn.ID); err != nil {
		fmt.Fprintln(os.Stderr, "failed to clean up admin user token: ", err)
	}

	os.Exit(ec)
}

func TestCompletionsDoNotRequireLogin(t *testing.T) {
	// skim out the list of completions
	selfOut, selfErr, err := execute(t, noAuth, "help", "completion")
	require.Nil(t, err)
	require.Empty(t, selfErr)
	_, after, found := strings.Cut(selfOut, "Actions")
	if !found {
		t.Fatal("failed to find subcommands by breaking on \"Actions\". stdout: ", selfOut)
	}
	_, after, _ = strings.Cut(after, "\n") // trim to JUST the actions
	for subcmd := range strings.SplitSeq(after, "\n") {
		subcmd = strings.TrimSpace(subcmd)
		t.Logf("testing subcommand \"%v\"", subcmd)
		selfOut, selfErr, err = execute(t, noAuth, "completion", subcmd)
		require.Nil(t, err)
		require.Empty(t, selfErr)
		require.NotEmpty(t, selfOut)
	}
}

func TestSelfSessionsMatchAdminSessions(t *testing.T) {
	// test that the `admin users sessions` action returns the same sessions as `self sessions`
	var (
		selfOut, selfErr   string
		selfExit           error
		adminOut, adminErr string
		adminExit          error
	)
	columnsArg := "--columns=UID,SessionID" // declare consistent columns

	selfOut, selfErr, selfExit = execute(t, admin_u_p, "users", "self", "sessions", "--csv", columnsArg)

	adminOut, adminErr, adminExit = execute(t, admin_u_p, "users", "sessions", "--csv", columnsArg, "1")

	assert.Nil(t, selfExit)
	assert.Empty(t, selfErr)
	assert.Nil(t, adminExit)
	assert.Empty(t, adminErr)

	// both stderrs should be empty
	if t.Failed() {
		t.FailNow()
	}

	// both stdouts should be csv-decodable
	selfCSV, err := csv.NewReader(strings.NewReader(selfOut)).ReadAll()
	require.Nil(t, err)
	require.Greater(t, len(selfCSV), 0)
	adminCSV, err := csv.NewReader(strings.NewReader(adminOut)).ReadAll()
	require.Nil(t, err)
	require.Greater(t, len(adminCSV), 0)
	if t.Failed() {
		t.FailNow()
	}
	// compare headers
	require.Equal(t, selfCSV[0], adminCSV[0], "headers mismatch")
	// compare bodies
	// TODO

}

// These tests are to ensure that help takes precedence over everything else in a command
func TestHelp(t *testing.T) {
	// we check for aspects of the help template, rather than exact-string-matching anything
	checkHelp := func(t *testing.T, stdout string) {
		stdout = strings.ToLower(stdout)
		assert.Contains(t, stdout, "synopsis")
		assert.Contains(t, stdout, "usage")
		assert.Contains(t, stdout, "global flags")
		if t.Failed() {
			t.Log("stdout: ", stdout)
		}
	}

	t.Run("root (as admin)", func(t *testing.T) {
		stdout, stderr, exit := execute(t, admin_u_p, "-h")
		require.Nil(t, exit)
		require.Empty(t, stderr)
		checkHelp(t, stdout)
	})
	t.Run("root (as non-admin)", func(t *testing.T) {
		stdout, stderr, exit := execute(t, second_u_p, "-h")
		require.Nil(t, exit)
		require.Empty(t, stderr)
		checkHelp(t, stdout)
	})

	t.Run("admin-gated nav", func(t *testing.T) {
		stdout, stderr, exit := execute(t, second_u_p, "admin", "-h")
		require.Nil(t, exit)
		require.Empty(t, stderr)
		checkHelp(t, stdout)
	})
	t.Run("admin-gated nested action", func(t *testing.T) {
		stdout, stderr, exit := execute(t, second_u_p, "admin", "license", "info", "-h")
		require.Nil(t, exit)
		require.Empty(t, stderr)
		checkHelp(t, stdout)
	})
}

func TestAdminGating(t *testing.T) {
	t.Run("non-gated actions are accessible to admins", func(t *testing.T) {
		stdout, stderr, exit := execute(t, admin_u_p, "query", "\"tag=gravwell | limit 1\"")
		require.Nil(t, exit, exit.Error())
		assert.Empty(t, stderr)
		assert.NotContains(t, stdout, "requires admin") // the requires admin error should be in stderr, but we check this just in case
	})
	t.Run("non-gated actions are accessible to non-admins", func(t *testing.T) {
		stdout, stderr, exit := execute(t, second_u_p, "query", "tag=gravwell | limit 1")
		require.Nil(t, exit, exit.Error())
		assert.Empty(t, stderr)
		assert.NotContains(t, stdout, "requires admin") // the requires admin error should be in stderr, but we check this just in case
	})
	t.Run("gated actions are accessible to admins", func(t *testing.T) {
		stdout, stderr, exit := execute(t, admin_u_p, "users", "list", "--csv", "--columns=ID,Username")
		require.Nil(t, exit)
		assert.Empty(t, stderr)
		assert.NotContains(t, stdout, "requires admin") // the requires admin error should be in stderr, but we check this just in case
		// sanity check that we got data by finding ourselves in that list
		rdr := csv.NewReader(strings.NewReader(stdout))
		header, err := rdr.Read()
		require.Nil(t, err)
		require.Len(t, header, 2)
		records, err := rdr.ReadAll()
		require.Nil(t, err)
		var found bool
		for _, record := range records {
			if record[0] == "1" && record[1] == "admin" {
				found = true
				break
			}
		}
		require.True(t, found, "failed to locate admin user in list of users: %v", records)
	})
	t.Run("gated actions are inaccessible to non-admins", func(t *testing.T) {
		stdout, stderr, exit := execute(t, second_u_p, "users", "list", "--csv", "--columns=ID,Username")
		assert.NotNil(t, exit)
		assert.Contains(t, stderr, "requires admin")
		assert.NotContains(t, stdout, "requires admin") // the requires admin error should be in stderr, but we check this just in case
	})
}

func TestNoLocalPermissions(t *testing.T) {
	restLogPath := filepath.Join(t.ArtifactDir(), "rest.log")
	findEndpoint := func(method, endpoint string) error {
		b, err := os.ReadFile(restLogPath)
		if err != nil {
			return err
		}
		found := strings.Contains(string(b), strings.ToUpper(method)+" "+endpoint)
		if found {
			return nil
		}
		return fmt.Errorf("rest log does not contain endpoint '%v'.", endpoint)
	}

	// this should fail like it does in TestAdminGating, but should issue a request to the backend.
	stdout, stderr, _ := execute(t, second_u_p, "--restlog="+restLogPath, "--no-local-permissions", "admin", "cleanup", "macros")
	// TODO basic actions need to be able to return errors.
	// Until then, we check stdout
	//assert.NotZero(t, exit)
	assert.Contains(t, stdout, "403")
	assert.NotContains(t, stdout, "requires admin")
	t.Log("stdout: ", stdout)
	t.Log("stderr: ", stderr)
	assert.Nil(t, findEndpoint(http.MethodDelete, grav.MACROS_URL))
	// this should react exactly like TestAdminGating does.
	//stdout, stderr, exit = execute(t, second_u_p, "--restlog="+restLogPath, "admin", "users")
}

// Fatal if the run fails.
func execute(t *testing.T, authMethod authMethod, args ...string) (stdout, stderr string, exitErr error) {
	t.Helper()
	var (
		metaArgs = argMetaBase
		env      = []string{cfgdir.EnvCfgDir + "=" + tCfgDir}
	)
	switch authMethod {
	case noAuth: // attach nothing
	case admin_u_p:
		metaArgs = append(metaArgs, "-u=admin")
		env = append(env, "GRAVWELL_PASSWORD=changeme")
	case second_u_p:
		metaArgs = append(metaArgs, "-u="+secondUser)
		env = append(env, "GRAVWELL_PASSWORD="+secondPass)
	case api:
		metaArgs = append(metaArgs, argAPI)
	}

	var sbOut, sbErr strings.Builder
	cmd := exec.CommandContext(t.Context(), binaryPath, append(metaArgs, args...)...)
	cmd.Stdout = &sbOut
	cmd.Stderr = &sbErr
	cmd.Env = env
	t.Log(cmd.String())
	err := cmd.Run()
	cmd.Wait()
	return sbOut.String(), sbErr.String(), err
}

/*const ( // testing server credentials
	user     = "admin"
	password = "changeme"
	server   = "localhost:80"
	apiKey   = "" // TODO
)

var realStderr, mockStderr, realStdout, mockStdout *os.File

func init() {
	// ensure we capture the normal STDOUT and STDERR so we can restore to them
	realStderr = os.Stderr
	realStdout = os.Stdout
}

// Tests the 'macro' action of gwcli
func TestMacros(t *testing.T) {

	pf := passfile(t, password)

	// connect to the server for manual calls
	testclient, err := grav.NewOpts(grav.Opts{Server: server, UseHttps: false, InsecureNoEnforceCerts: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = testclient.Login(user, password); err != nil {
		t.Fatal(err)
	}

	t.Run("macros list --csv", func(t *testing.T) {
		// generate results manually, for comparison
		// get the current list of macros so we can validate that gwcli turned back the same ones
		macros, err := testclient.ListMacros(nil)
		if err != nil {
			t.Fatal(err)
		}
		columns := []string{"UID", "Global", "Name"}
		want := strings.TrimSpace(weave.ToCSV(macros.Results, columns,
			weave.CSVOptions{}))

		// run the test body
		cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" macros list --"+ft.CSV.Name()+" --"+ft.SelectColumns.Name()+"=%s", user, pf, strings.Join(columns, ","))
		statusCode, stdout, stderr := executeCmd(t, cmd)

		// check the outcome
		testsupport.NonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "", stderr)
		checkResult(t, true, "stdout", want, strings.TrimSpace(stdout))
	})

	t.Run("macros create", func(t *testing.T) {
		var (
			macroName = randomdata.SillyName()
			macroDesc = "macro created for automated testing"
			macroExp  = "testexpand"
		)
		// fetch the number of macros prior to creation
		priorMacros, err := testclient.ListMacros(nil)
		if err != nil {
			panic(err)
		}

		// ensure the macro DNE, reroll it if it does
		for {
			if slices.ContainsFunc(priorMacros.Results, func(sm types.Macro) bool {
				return macroName == sm.Name
			}) {
				//reroll name
				macroName = randomdata.SillyName()
				continue
			}
			break
		}

		// create a new macro from the cli, in script mode
		cmd := fmt.Sprintf("-u %s --password %s --insecure --"+ft.NoInteractive.Name()+" macros create --name %s --description %s --expansion %s", user, password, macroName, macroDesc, macroExp)
		statusCode, _, stderr := executeCmd(t, cmd)
		testsupport.NonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "", stderr)
		// refetch macros to check the count has increased by one
		postMacros, err := testclient.ListMacros(nil)
		if err != nil {
			panic(err)
		}
		if len(postMacros.Results) != len(priorMacros.Results)+1 {
			t.Fatalf("expected post-create macros len(%v) == pre-create macros len(%v)+1 ", len(postMacros.Results), len(priorMacros.Results))
		}
		// TODO parse out macro ID from stdout and ensure it exists in the postMacros list
	})

	t.Run("macros list "+ft.JSON.Name(), func(t *testing.T) {
		// generate results manually, for comparison
		// get the current list of macros so we can validate that gwcli turned back the same ones
		macros, err := testclient.ListMacros(nil)
		if err != nil {
			t.Fatal(err)
		}
		columns := []string{"UID", "Global", "Name", "WriteAccess.GIDs", "Description", "Expansion", "Labels"}
		var want string
		if json, err := weave.ToJSON(macros.Results, columns, weave.JSONOptions{}); err != nil {
			t.Fatal(err)
		} else {
			want = strings.TrimSpace(json)
			if want == "" { // empty list command outputs "no data found"
				want = "no data found"
			}
		}

		cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" macros list --"+ft.JSON.Name()+" --"+ft.SelectColumns.Name()+"=%s", user, pf, strings.Join(columns, ","))
		statusCode, stdout, stderr := executeCmd(t, cmd)

		// check the outcome
		testsupport.NonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "", stderr)
		checkResult(t, true, "stdout", want, strings.TrimSpace(stdout))
	})

	t.Run("macros delete (dryrun)", func(t *testing.T) {
		// fetch the macros prior to deletion
		priorMacros, err := testclient.ListMacros(nil)
		if err != nil {
			panic(err)
		}
		if len(priorMacros.Results) < 1 {
			t.Skip("no macros to delete")
		}
		// pick a macro for faux-deletion
		toDeleteID := priorMacros.Results[0].ID
		t.Logf("Selecting macro %v (ID: %v) for faux-deletion", priorMacros.Results[0].Name, priorMacros.Results[0].ID)

		cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" macros delete --"+ft.Dryrun.Name()+" --id=%d", user, pf, toDeleteID)
		statusCode, _, stderr := executeCmd(t, cmd)

		// check the outcome
		testsupport.NonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "", stderr)

		// refetch macros to check that count hasn't changed
		postMacros, err := testclient.ListMacros(nil)
		if err != nil {
			t.Fatal(err)
		} else if len(postMacros.Results) != len(priorMacros.Results) {
			t.Fatalf("expected macro count to not change. post count: %v, pre count: %v",
				len(postMacros.Results), len(priorMacros.Results))
		}
		// ensure the selected macro still exists
		var found = false
		for _, m := range postMacros.Results {
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

		// fetch the macros prior to deletion
		priorMacros, err := testclient.ListMacros(nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(priorMacros.Results) < 1 {
			t.Skip("no macros to delete")
		}

		cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" macros delete", user, pf)
		statusCode, stdout, stderr := executeCmd(t, cmd)

		// check the outcome
		testsupport.NonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stdout", "", stdout)

		// refetch macros to check that count hasn't changed
		postMacros, err := testclient.ListMacros(nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(postMacros.Results) != len(priorMacros.Results) {
			t.Fatalf("expected macro count to not change. post count: %v, pre count: %v",
				len(postMacros.Results), len(priorMacros.Results))
		}
	})

	t.Run("macros delete", func(t *testing.T) {
		// fetch the macros prior to deletion
		priorMacros, err := testclient.ListMacros(nil)
		if err != nil {
			panic(err)
		}
		if len(priorMacros.Results) < 1 {
			t.Skip("no macros to delete")
		}
		// pick a macro for deletion
		toDeleteID := priorMacros.Results[0].ID
		t.Logf("Selecting macro %v (ID: %v) for deletion", priorMacros.Results[0].Name, priorMacros.Results[0].ID)

		cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" macros delete --id %v", user, pf, toDeleteID)
		statusCode, _, stderr := executeCmd(t, cmd)

		// check the outcome
		testsupport.NonZeroExit(t, statusCode, stderr)

		// refetch macros to check the count has decreased by one
		postMacros, err := testclient.ListMacros(nil)
		if err != nil {
			t.Fatal(err)
		} else if len(postMacros.Results) != len(priorMacros.Results)-1 {
			t.Fatalf("expected post-delete macros len (%v) == pre-delete macros len-1 (%v)", len(postMacros.Results), len(priorMacros.Results))
		}
		// ensure the correct macro was deleted
		for _, m := range postMacros.Results {
			if m.ID == toDeleteID {
				t.Log("ID of deletion attempt found still alive.")
				t.Log("priorMacros:\n")
				for _, prior := range priorMacros.Results {
					t.Logf("%v (ID: %v)\n", prior.Name, prior.ID)
				}
				t.Log("postMacros:\n")
				for _, post := range postMacros.Results {
					t.Logf("%v (ID: %v)\n", post.Name, post.ID)
				}
				t.FailNow()
			}
		}
	})

}

func TestQueries(t *testing.T) {

	pf := passfile(t, password)

	// connect to the server for manual calls
	testclient, err := grav.NewOpts(grav.Opts{Server: server, UseHttps: false, InsecureNoEnforceCerts: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = testclient.Login(user, password); err != nil {
		t.Fatal(err)
	}

	t.Run("query output json to file", func(t *testing.T) {
		outPath := path.Join(t.TempDir(), "out.json")
		qry := "tag=gravwell"

		// TODO need to make sure -o is valid before submitting the query
		cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" query %s -o %s --"+ft.JSON.Name(), user, pf, qry, outPath)
		statusCode, stdout, stderr := executeCmd(t, cmd)
		testsupport.NonZeroExit(t, statusCode, stderr)
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
				if !jsontext.Value(record).IsValid(jsonv2opts.Wire) && string(record) != "[]" { // Go does not consider '[]' valid JSON, but we do
					t.Errorf("'%v' is not valid JSON", record)
				}
			}
			// check the record count matches the search's item count
			if count != uint(si.ItemCount) {
				t.Fatalf("incorrect item count in file: %s", testsupport.ExpectedActual(si.ItemCount, count))
			}
		}
	})

	t.Run("background query 'tags=gravwell limit 3'", func(t *testing.T) {
		outPath := path.Join(t.TempDir(), "IShouldNotBeCreated.txt")
		qry := "tag=gravwell"

		cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" query %s -o %s --background", user, pf, qry, outPath)
		statusCode, stdout, stderr := executeCmd(t, cmd)
		testsupport.NonZeroExit(t, statusCode, stderr)
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
		cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" query %s -o %s --append", user, pf, qry, outPath)
		statusCode, _, stderr := executeCmd(t, cmd)
		testsupport.NonZeroExit(t, statusCode, stderr)
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
		qry := "tag=gravwell limit 1"
		cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" query %s --"+ft.CSV.Name(), user, pf, qry)
		statusCode, stdout, stderr := executeCmd(t, cmd)
		testsupport.NonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "", stderr)

		if stdout != "" { // if results were returned, test them further for csv parse-ability
			// strip the header off the output
			_, stdout, found := strings.Cut(stdout, "\n")
			if !found {
				t.Fatalf("expected a header line of column; found no newline to break on. output: %v", stdout)
			}

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
						// if this is the header line, ignore it

						t.Log("all output:", stdout)
						t.Fatalf("bad csv record '%v': %v", r, err)
					}
				}
			}
		}

	})

	// FIXME  Commenting due to breakage incoming from https://github.com/gravwell/issues/issues/2144
	// t.Run("attach to backgrounded, stdout", func(t *testing.T) {
	// 	var sid string
	// 	{ // submit a background query
	// 		bgQry := "tag=gravwell limit 1 | sleep 5s"
	// 		// parse the query, as this will tell us early if sleep is not available (aka we are not in a debug build)
	// 		if err := testclient.ParseSearch(bgQry); err != nil {
	// 			t.Skip("background query could be not parsed: ", err)
	// 		}
	//
	// 		cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" query %s --background", user, pf, bgQry)
	// 		statusCode, stdout, stderr := executeCmd(t, cmd)
	// 		testsupport.NonZeroExit(t, statusCode, stderr)
	// 		checkResult(t, false, "stderr", "", stderr)
	//
	// 		// save off background query sid
	// 		sid = skimSID(t, stdout)
	// 		if sid == "" {
	// 			t.Fatal("failed to scan search ID out of stdout")
	// 		}
	// 		t.Logf("scanned out sid %s", sid)
	// 	}
	//
	// 	// attach to background query
	// 	cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" queries attach %s", user, pf, sid)
	// 	statusCode, attachSTDOUT, stderr := executeCmd(t, cmd)
	// 	testsupport.NonZeroExit(t, statusCode, stderr)
	// 	checkResult(t, false, "stderr", "", stderr)
	//
	// 	// fetch the background query's results manually
	// 	var actualOut string
	// 	{
	// 		var sb strings.Builder
	// 		rc, err := testclient.DownloadSearch(sid, types.TimeRange{}, "text")
	// 		if err != nil {
	// 			t.Fatal("failed to manually fetch query results: ", err)
	// 		}
	// 		written, err := io.Copy(&sb, rc)
	// 		if err != nil {
	// 			t.Fatal(err)
	// 		}
	// 		actualOut = sb.String()
	// 		if written == 0 { // if the data was empty, set it to our empty statement
	// 			actualOut = querysupport.NoResults
	// 		}
	// 	}
	// 	// check stdout
	// 	// don't really care about the data, just that it matches what it should
	// 	if attachSTDOUT != actualOut {
	// 		t.Fatalf("attach pulled back different results from query (sid=%v).%v", sid, testsupport.ExpectedActual(actualOut, attachSTDOUT))
	// 	}
	// })

	// FIXME  Commenting due to breakage incoming from https://github.com/gravwell/issues/issues/2144
	// t.Run("attach to backgrounded, file", func(t *testing.T) {
	//
	// 	var sid string
	// 	{ // submit a background query
	// 		bgQry := "tag=gravwell limit 1 | sleep 5s"
	// 		// parse the query, as this will tell us early if sleep is not available (aka we are not in a debug build)
	// 		if err := testclient.ParseSearch(bgQry); err != nil {
	// 			t.Skip("background query could be not parsed: ", err)
	// 		}
	//
	// 		cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" query %s --background", user, pf, bgQry)
	// 		statusCode, stdout, stderr := executeCmd(t, cmd)
	// 		testsupport.NonZeroExit(t, statusCode, stderr)
	// 		checkResult(t, false, "stderr", "", stderr)
	//
	// 		// save off background query sid
	// 		sid = skimSID(t, stdout)
	// 		if sid == "" {
	// 			t.Fatal("failed to scan search ID out of stdout")
	// 		}
	// 		t.Logf("scanned out sid %s", sid)
	// 	}
	//
	// 	// attach to background query
	// 	outPath := path.Join(t.TempDir(), "out.txt")
	// 	cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" queries attach %s -o %s", user, pf, sid, outPath)
	// 	statusCode, _, stderr := executeCmd(t, cmd)
	// 	testsupport.NonZeroExit(t, statusCode, stderr)
	// 	checkResult(t, false, "stderr", "", stderr)
	//
	// 	// fetch the background query's results manually
	// 	var correctOut string
	// 	{
	// 		var sb strings.Builder
	// 		rc, err := testclient.DownloadSearch(sid, types.TimeRange{}, "text")
	// 		if err != nil {
	// 			t.Fatal("failed to manually fetch query results: ", err)
	// 		}
	// 		written, err := io.Copy(&sb, rc)
	// 		if err != nil {
	// 			t.Fatal(err)
	// 		}
	// 		correctOut = sb.String()
	// 		if written == 0 { // if the data was empty, set it to our empty statement
	// 			correctOut = querysupport.NoResults
	// 		}
	// 	}
	//
	// 	// slurp the file
	// 	fileBytes, err := os.ReadFile(outPath)
	// 	if err != nil {
	// 		t.Fatal(err)
	// 	}
	//
	// 	// check stdout
	// 	// don't really care about the data, just that it matches what it should
	// 	if string(fileBytes) != correctOut {
	// 		t.Fatalf("attach pulled back different results from query (sid=%v).%v", sid, testsupport.ExpectedActual(correctOut, string(fileBytes)))
	// 	}
	// })

	// FIXME  Commenting due to breakage incoming from https://github.com/gravwell/issues/issues/2144
	// t.Run("attach to backgrounded after completion, stdout", func(t *testing.T) {
	// 	var sid string
	// 	{ // submit a background query
	// 		bgQry := "tag=gravwell limit 1 | sleep 5s"
	// 		// parse the query, as this will tell us early if sleep is not available (aka we are not in a debug build)
	// 		if err := testclient.ParseSearch(bgQry); err != nil {
	// 			t.Skip("background query could be not parsed: ", err)
	// 		}
	//
	// 		cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" query %s --background", user, pf, bgQry)
	// 		statusCode, stdout, stderr := executeCmd(t, cmd)
	// 		testsupport.NonZeroExit(t, statusCode, stderr)
	// 		checkResult(t, false, "stderr", "", stderr)
	//
	// 		// save off background query sid
	// 		sid = skimSID(t, stdout)
	// 		if sid == "" {
	// 			t.Fatal("failed to scan search ID out of stdout")
	// 		}
	// 		t.Logf("scanned out sid %s", sid)
	// 	}
	//
	// 	// wait for query before attaching
	// 	time.Sleep(5 * time.Second)
	//
	// 	// attach to background query
	// 	cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" queries attach %s", user, pf, sid)
	// 	statusCode, cmdOut, stderr := executeCmd(t, cmd)
	// 	testsupport.NonZeroExit(t, statusCode, stderr)
	// 	checkResult(t, false, "stderr", "", stderr)
	//
	// 	// fetch the background query's results manually
	// 	var correctOut string
	// 	{
	// 		var sb strings.Builder
	// 		rc, err := testclient.DownloadSearch(sid, types.TimeRange{}, "text")
	// 		if err != nil {
	// 			t.Fatal("failed to manually fetch query results: ", err)
	// 		}
	// 		written, err := io.Copy(&sb, rc)
	// 		if err != nil {
	// 			t.Fatal(err)
	// 		}
	// 		correctOut = sb.String()
	// 		if written == 0 { // if the data was empty, set it to our empty statement
	// 			correctOut = querysupport.NoResults
	// 		}
	// 	}
	// 	// check stdout
	// 	// don't really care about the data, just that it matches what it should
	// 	if cmdOut != correctOut {
	// 		t.Fatalf("attach pulled back different results from query (sid=%v).%v", sid, testsupport.ExpectedActual(correctOut, cmdOut))
	// 	}
	// })

	// FIXME  Commenting due to breakage incoming from https://github.com/gravwell/issues/issues/2144
	// t.Run("attach to foregrounded after completion, stdout", func(t *testing.T) {
	// 	// parse the query, as this will tell us early if sleep is not available (aka we are not in a debug build)
	// 	fgQry := "tag=gravwell limit 1 | sleep 5s"
	// 	if err := testclient.ParseSearch(fgQry); err != nil {
	// 		t.Skip("foreground query could be not parsed: ", err)
	// 	}
	//
	// 	s, err := testclient.StartSearch(fgQry, time.Now().Add(-time.Minute), time.Now(), false)
	// 	if err != nil {
	// 		t.Fatal(err)
	// 	}
	// 	sid := s.ID
	//
	// 	// give the query time to start before attaching
	// 	time.Sleep(300 * time.Millisecond)
	//
	// 	// attach to background query
	// 	cmd := fmt.Sprintf("-u %s -p %s --insecure --"+ft.NoInteractive.Name()+" queries attach %s", user, pf, sid)
	// 	statusCode, cmdOut, stderr := executeCmd(t, cmd)
	// 	testsupport.NonZeroExit(t, statusCode, stderr)
	// 	checkResult(t, false, "stderr", "", stderr)
	//
	// 	// fetch the background query's results manually
	// 	var correctOut string
	// 	{
	// 		var sb strings.Builder
	// 		rc, err := testclient.DownloadSearch(sid, types.TimeRange{}, "text")
	// 		if err != nil {
	// 			t.Fatal("failed to manually fetch query results: ", err)
	// 		}
	// 		written, err := io.Copy(&sb, rc)
	// 		if err != nil {
	// 			t.Fatal(err)
	// 		}
	// 		correctOut = sb.String()
	// 		if written == 0 { // if the data was empty, set it to our empty statement
	// 			correctOut = querysupport.NoResults
	// 		}
	// 	}
	// 	// check stdout
	// 	// don't really care about the data, just that it matches what it should
	// 	if cmdOut != correctOut {
	// 		t.Fatalf("attach pulled back different results from query (sid=%v).%v", sid, testsupport.ExpectedActual(correctOut, cmdOut))
	// 	}
	// })
}

//#endregion

// Tests focusing on ensuring proper, external login logic.
func TestLogin(t *testing.T) {
	t.Run("login via full cred, no MFA", func(t *testing.T) {
		// issue the my info command to confirm we are logged into the correct user
		cmd := fmt.Sprintf("-u %s --password %s --insecure --"+ft.NoInteractive.Name()+" user myinfo --"+ft.CSV.Name(), user, password)
		statusCode, cmdOut, stderr := executeCmd(t, cmd)
		testsupport.NonZeroExit(t, statusCode, stderr)
		checkResult(t, false, "stderr", "", stderr)

		// check that the output is valid CSV
		csvR := csv.NewReader(strings.NewReader(cmdOut))
		records, err := csvR.ReadAll()
		if err != nil {
			t.Fatal(err)
		} else if len(records) != 2 { // check that we have exactly 2 lines (a header line and 1 data line)
			t.Fatal("bad line count.", testsupport.ExpectedActual(2, len(records)))
		}
		// walk the header line for username's index
		idx := slices.Index(records[0], "User")
		if idx == -1 {
			t.Fatal("found no 'User' column")
		}
		username := records[1][idx]
		if user != username {
			t.Fatal(testsupport.ExpectedActual(user, username))
		}
	})
}

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
	//var (
	//	fgbg    string // unused
	//	numeric uint64
	//)
	for res := range resultsOut {
		t.Logf("scanning line '%s'", res)

		match := sidRGX.FindStringSubmatch(res)
		if match != nil {
			return match[1] // want the first capture group
		}
	}

	return ""
}

// Generates a passfile in the temp directory, returning its full path.
func passfile(t *testing.T, password string) string {
	t.Helper()

	fp := path.Join(t.TempDir(), "passfile")
	f, err := os.Create(fp)
	if err != nil {
		t.Fatalf("failed to create passfile: %v", err)
	}
	if _, err := f.WriteString(password); err != nil {
		t.Fatalf("failed to write passfile: %v", err)
	}

	f.Sync()
	f.Close()

	return fp
}

// #region strings and failure checks

// Fails if expected != actual.
// source is probably "stderr" or "stdout".
// If fatal, test execution will stop.
func checkResult(t *testing.T, fatal bool, source, expected, actual string) {
	t.Helper()

	if expected != actual {
		if fatal {
			t.Fatalf("bad %s: %s", source, testsupport.ExpectedActual(expected, actual))
		} else {
			t.Errorf("bad %s: %s", source, testsupport.ExpectedActual(expected, actual))
		}
	}
}

// #endregion
*/
