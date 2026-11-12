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
