/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Assumes a gravwell instance is running at `server` endpoint with credentials `user`, `pass`.
package query

import (
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"

	"github.com/spf13/cobra"
)

const (
	server = "localhost:80"
	user   = "admin"
	pass   = "changeme"
)

/*
// UUIDs are not seeded, so make sure the uuid1str const actually exists on the gravwell server.
// Unsetting the constant skips tests that require it

	func TestGenerateQueryString(t *testing.T) {
		// initialize clilogger
		var logFile = path.Join(os.TempDir(), "gwcli.Test_GenerateQueryString.log")
		clilog.Init(logFile, "DEBUG")

		const uuid1str = "" // ex: 52985695-ae81-4e82-ba1d-bce54f96def7

		var restLogFile = path.Join(os.TempDir(), "gwcli.Test_GenerateQueryString.rest.log")

		if err := connection.Initialize(server, false, true, restLogFile); err != nil {
			panic(err)
		}
		if err := connection.Login(connection.Credentials{Username: user, Password: pass}, true); err != nil {
			panic(err)
		}

		type args struct {
			// args managed by the cobra.Command, such as flags
			// global flags are assumed to be managed (per the constant above)
			flagArgs []string
			args     []string // leftover, positional arguments cobra would pass here after parsing
		}
		tests := []struct {
			name      string
			args      args
			wantQuery string
			wantErr   bool
			skip      bool
		}{
			{
				name:      "basic argument query",
				args:      args{[]string{}, []string{"tag=gravwell"}},
				wantQuery: "tag=gravwell", wantErr: false,
			},
			{
				name:      "basic multiword argument query",
				args:      args{[]string{}, []string{"tag=dpkg words status"}},
				wantQuery: "tag=dpkg words status", wantErr: false,
			},
			{
				name:      "uuid " + uuid1str,
				args:      args{[]string{"-r", uuid1str}, []string{}},
				wantQuery: "tag=gravwell", wantErr: false,
				skip: uuid1str == "", // skip if constant is unset
			},
			{
				name:      "invalid uuid 'all-hail-the-gopher'",
				args:      args{[]string{"-r", "all-hail-the-gopher"}, []string{}},
				wantQuery: "", wantErr: true,
				skip: uuid1str == "", // skip if constant is unset
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if tt.skip {
					t.SkipNow()
				}
				cmd := generateCobraCommand(tt.args.flagArgs)

				gotQuery, err := fetchQueryString(cmd.Flags(), tt.args.args)
				if (err != nil) != tt.wantErr {
					t.Errorf("GenerateQueryString() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if gotQuery != tt.wantQuery {
					t.Errorf("GenerateQueryString() = %v, want %v", gotQuery, tt.wantQuery)
				}
			})
		}
	}
*/
func Test_tryQuery(t *testing.T) {
	var (
		logFile     = path.Join(os.TempDir(), "gwcli.Test_tryQuery.log")
		restLogFile = path.Join(os.TempDir(), "gwcli.Test_tryQuery.rest.log")
	)

	// establish cli writer
	clilog.Init(logFile, "DEBUG")

	// establish connection
	if err := connection.Initialize(server, false, true, restLogFile); err != nil {
		panic(err)
	}
	if err := connection.Login(connection.Credentials{Username: user, Password: pass}, true); err != nil {
		panic(err)
	}

	type args struct {
		qry      string
		duration time.Duration
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name:    "invalid query",
			args:    args{qry: "tags=gravwelll", duration: -2 * time.Hour},
			wantErr: true,
		},
		{
			name:    "whitespace query",
			args:    args{qry: " ", duration: -2 * time.Hour},
			wantErr: true,
		},
		{
			name:    "positive duration",
			args:    args{qry: " ", duration: 2 * time.Hour},
			wantErr: true,
		},
		{
			name:    "valid",
			args:    args{qry: "tag=gravwell", duration: -30 * time.Minute},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := connection.StartQuery(tt.args.qry, tt.args.duration)
			if err != nil {
				if tt.wantErr {
					return
				}
				t.Errorf("tryQuery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// unable to really compare search structs returned,
			// just check they were created as expected
			if got.ID == "" || got.SearchString != tt.args.qry {
				t.Errorf("tryQuery() invalid search struct: got struct %v", got)
				return
			}
		})
	}
}

// Simple tests to check for basic functionality without deep checking the results.
// Primarily checking that data was successfully put to a file or the terminal.
func Test_run(t *testing.T) {
	var (
		logFile     = path.Join(os.TempDir(), "gwcli.Test_run.log")
		restLogFile = path.Join(os.TempDir(), "gwcli.Test_run.rest.log")
	)

	// establish connection
	if err := connection.Initialize(server, false, true, restLogFile); err != nil {
		panic(err)
	}
	if err := connection.Login(connection.Credentials{Username: user, Password: pass}, true); err != nil {
		panic(err)
	}
	// establish cli writer
	clilog.Init(logFile, "DEBUG")

	prepCmd := func(flagArgs []string) *cobra.Command {
		// setup the command instance
		cmd := cobra.Command{Use: "test"}

		fs := initialLocalFlagSet()
		cmd.Flags().AddFlagSet(&fs)
		// mock the root persistent flags that should have been passed down
		cmd.Flags().Bool("script", false,
			"disallows gwcli from entering interactive mode and prints context help instead.\n"+
				"Recommended for use in scripts to avoid hanging on a malformed command.")
		cmd.Flags().StringP("username", "u", "", "login credential.")
		cmd.Flags().StringP("password", "p", "", "login credential.")
		cmd.Flags().Bool("no-color", false, "disables colourized output.")
		cmd.Flags().String("server", "localhost:80", "<host>:<port> of instance to connect to.\n")
		cmd.Flags().StringP("log", "l", "./gwcli.log", "log location for developer logs.\n")
		cmd.Flags().String("loglevel", "DEBUG", "log level for developer logs (-l).\n"+
			"Possible values: 'OFF', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'CRITICAL', 'FATAL'.\n")
		cmd.Flags().Bool("insecure", false, "do not use HTTPS and do not enforce certs.")
		cmd.ParseFlags(flagArgs)
		return &cmd
	}

	t1 := "Test_run.output-to-file.results.txt"
	t.Run("output to file '"+t1+"'", func(t *testing.T) {
		flagArgs := strings.Split("-o "+t1+" --script", " ")
		args := strings.Split("tag=gravwell", " ")

		cmd := prepCmd(flagArgs)

		// run
		run(cmd, args)

		// check that the expected file exists and has data
		fileInfo, err := os.Stat(t1)
		if err != nil {
			t.Fatalf("Failed to stat file %s: %v", t1, err)
		}
		if fileInfo.Size() == 0 {
			t.Errorf("File has no contents")
		}
		os.Remove(t1)
	})

	t2 := "Test_run.output-to-file.results.json"
	t.Run("output to file '"+t2+"'", func(t *testing.T) {
		flagArgs := strings.Split("-o "+t2+" --script --json", " ")
		args := strings.Split("tag=gravwell", " ")

		cmd := prepCmd(flagArgs)

		// run
		run(cmd, args)

		// check that the expected file exists and has data
		fileInfo, err := os.Stat(t2)
		if err != nil {
			t.Fatalf("Failed to stat file %s: %v", t2, err)
		}
		if fileInfo.Size() == 0 {
			t.Errorf("File has no contents")
		}
		os.Remove(t2)
	})

	// close the connection
	connection.End()

	// clean up log
	os.Remove(logFile)
}

//#region helpers

func generateCobraCommand(args []string) *cobra.Command {
	cmd := cobra.Command{Use: "test"}

	fs := initialLocalFlagSet()
	cmd.Flags().AddFlagSet(&fs)

	// this cmd isn't being executed, so we have to call parse manually

	cmd.ParseFlags(args)

	return &cmd
}

//#endregion
