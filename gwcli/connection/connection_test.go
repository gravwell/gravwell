//go:build !ci
// +build !ci

/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package connection_test

import (
	"os"
	"path"
	"testing"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
)

const (
	server          = "localhost:80"
	username string = "admin"
	password string = "changeme"
)

func TestLogin(t *testing.T) {
	// create a passfile we can use
	var passfileSkip = false
	pfPath := path.Join(t.TempDir(), password)
	pf, err := os.Create(pfPath)
	if err != nil {
		t.Logf("failed to create passfile @ %v: %v", pfPath, err)
		passfileSkip = true
	}
	if _, err := pf.WriteString(password); err != nil {
		t.Logf("failed to write passfile @ %v: %v", pfPath, err)
		passfileSkip = true
	}

	// setup singletons
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "DEBUG"); err != nil {
		t.Fatalf("%v", err)
	}
	if err := connection.Initialize(server, false, true, path.Join(t.TempDir(), "rest.log")); err != nil {
		panic(err)
	}

	type args struct {
		cred       connection.Credentials
		scriptMode bool
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"script mode: valid username and password", args{connection.Credentials{"admin", password, ""}, true}, false},
		{"script mode: valid username and passfile", args{connection.Credentials{"admin", "", pfPath}, true}, false},
		// cannot use this test, as Cobra tests our flags and Cobra is not being invoked
		//{"script mode: password and passfile given", args{Credentials{"admin", password, pfPath}, true}, true},
		{"script mode: only password given", args{connection.Credentials{"", password, ""}, true}, true},
		{"script mode: only passfile given", args{connection.Credentials{"", "", pfPath}, true}, true},
	}

	for _, tt := range tests {
		// destroy the Client singleton between each test
		if connection.Client != nil {
			connection.Client.Logout()
		}

		t.Run(tt.name, func(t *testing.T) {
			// if we failed to create the passfile, but this test relies on it, skip the test
			if passfileSkip {
				t.SkipNow()
			}
			if err := connection.Login(tt.args.cred, tt.args.scriptMode); (err != nil) != tt.wantErr {
				t.Fatalf("Login() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// check that we can actually do something
				if _, err := connection.Client.MyInfo(); err != nil {
					t.Fatalf("failed to fetch client info: %v", err)
				}
			}

		})
	}

	t.Run("u/p login -> token login -> different u/p login", func(t *testing.T) {
		// spin up a test client
		testclient, err := grav.NewOpts(grav.Opts{Server: server, UseHttps: false, InsecureNoEnforceCerts: true})
		if err != nil {
			t.Fatal(err)
		}
		if err = testclient.Login(username, password); err != nil {
			t.Fatal(err)
		}
		// create a second user for us to use
		secondU, secondP := "Milly", "LooLooLand"
		if err := testclient.AddUser(secondU, secondP, "Mildred Knolastname", "milly@imp.com", false); err != nil {
			t.Fatal(err)
		}

		// destroy the Client singleton between each test
		if connection.Client != nil {
			connection.Client.Logout()
		}

		// reinitialize the client
		if err := connection.Initialize(server, false, true, path.Join(t.TempDir(), "rest.log")); err != nil {
			panic(err)
		}

		// ensure no token exists
		//cfgdir.DefaultTokenPath

		// sign in using credentials

	})

}
