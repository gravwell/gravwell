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
	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
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

		// ensure there is a second user for us to test against
		secondU, secondP := "Milly", "LooLooLand"
		if _, err := testclient.LookupUser(secondU); err != nil { // check if the user already exists (such as from running this test multiple times)
			t.Logf("failed to lookup user %v, attempting creation...", secondU)
			if err := testclient.AddUser(secondU, secondP, "Mildred Knolastname", "milly@imp.com", false); err != nil {
				t.Fatal(err)
			}
		}

		// destroy the Client singleton between each test
		if connection.Client != nil {
			connection.Client.Logout()
		}

		// reinitialize the client
		if err := connection.Initialize(server, false, true, path.Join(t.TempDir(), "rest.log")); err != nil {
			t.Fatal(err)
		}

		// ensure no token exists
		if err := os.Remove(cfgdir.DefaultTokenPath); err != nil {
			t.Fatal(err)
		}

		// sign into the default account using credentials
		initLogin(t, username, password)

		// ensure we can make a couple calls
		if info, err := connection.Client.MyInfo(); err != nil {
			t.Fatal("failed to make call after logging in via credentials: ", err)
		} else if _, err := connection.Client.GetUserMacros(info.UID); err != nil {
			t.Fatal("failed to make call after logging in via credentials: ", err)
		}

		// shutter the connection
		connection.End()

		// ensure we are unable to make a call
		if info, err := connection.Client.MyInfo(); err == nil {
			t.Fatalf("expected to receive an error after shuttering connection, but call successfully returned info (%v)", info)
		}

		// sign into the default account without credentials
		initLogin(t, "", "")

		// ensure we can make a couple calls
		if info, err := connection.Client.MyInfo(); err != nil {
			t.Fatal("failed to make call after logging in via token: ", err)
		} else if _, err := connection.Client.GetUserMacros(info.UID); err != nil {
			t.Fatal("failed to make call after logging in via token: ", err)
		}

		// shutter the connection
		connection.End()

		// ensure we are unable to make a call
		if info, err := connection.Client.MyInfo(); err == nil {
			t.Fatalf("expected to receive an error after shuttering connection, but call successfully returned info (%v)", info)
		}

		// sign in as a different user
		initLogin(t, secondU, secondP)

		// ensure we can make a couple calls
		if info, err := connection.Client.MyInfo(); err != nil {
			t.Fatal("failed to make call after logging in second user via credentials: ", err)
		} else if info.User != secondU { // ensure we got the correct user
			t.Fatalf("logged in as %v, expected to log in as %v", info.User, secondU)
		} else if _, err := connection.Client.GetUserMacros(info.UID); err != nil {
			t.Fatal("failed to make call after logging in second user via credentials: ", err)
		}

		// shutter the connection
		connection.End()

		// ensure we are unable to make a call
		if info, err := connection.Client.MyInfo(); err == nil {
			t.Fatalf("expected to receive an error after shuttering connection, but call successfully returned info (%v)", info)
		}

		// ensure the token has updated to our second user
		initLogin(t, "", "")

		// ensure we can make a couple calls
		if info, err := connection.Client.MyInfo(); err != nil {
			t.Fatal("failed to make call after logging in second user via token: ", err)
		} else if info.User != secondU { // ensure we got the correct user
			t.Fatalf("logged in as %v, expected to log in as %v", info.User, secondU)
		} else if _, err := connection.Client.GetUserMacros(info.UID); err != nil {
			t.Fatal("failed to make call after logging in second user via token: ", err)
		}

	})

}

// Initializes and logs, calling fatal on the first error
func initLogin(t *testing.T, u, p string) {
	if err := connection.Initialize(server, false, true, path.Join(t.TempDir(), "rest.log")); err != nil {
		t.Fatal(err)
	}

	if err := connection.Login(connection.Credentials{u, p, ""}, true); err != nil {
		t.Fatal(err)
	}

}
