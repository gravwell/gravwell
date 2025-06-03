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
	server = "localhost:80"
	// default user
	defaultUser string = "admin"
	defaultPass string = "changeme"
	// second user, created and deleted between tests
	altUser string = "Milly"
	altPass string = "LooLooLand"
)

func TestLogin(t *testing.T) {
	// create a passfile we can use
	var passfileSkip = false
	pfPath := path.Join(t.TempDir(), defaultPass)
	pf, err := os.Create(pfPath)
	if err != nil {
		t.Logf("failed to create passfile @ %v: %v", pfPath, err)
		passfileSkip = true
	}
	if _, err := pf.WriteString(defaultPass); err != nil {
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
		{"script mode: valid username and password", args{connection.Credentials{"admin", defaultPass, ""}, true}, false},
		{"script mode: valid username and passfile", args{connection.Credentials{"admin", "", pfPath}, true}, false},
		// cannot use this test, as Cobra tests our flags and Cobra is not being invoked
		//{"script mode: password and passfile given", args{Credentials{"admin", password, pfPath}, true}, true},
		{"script mode: only password given", args{connection.Credentials{"", defaultPass, ""}, true}, true},
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
		if err = testclient.Login(defaultUser, defaultPass); err != nil {
			t.Fatal(err)
		}

		// ensure there is a second user for us to test against
		createAltUser(t, testclient, false)
		t.Cleanup(func() { deleteAltUser(t, testclient) })

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
		initLogin(t, defaultUser, defaultPass)

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
		initLogin(t, altUser, altPass)

		// ensure we can make a couple calls
		if info, err := connection.Client.MyInfo(); err != nil {
			t.Fatal("failed to make call after logging in second user via credentials: ", err)
		} else if info.User != altUser { // ensure we got the correct user
			t.Fatalf("logged in as %v, expected to log in as %v", info.User, altUser)
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
		} else if info.User != altUser { // ensure we got the correct user
			t.Fatalf("logged in as %v, expected to log in as %v", info.User, altUser)
		} else if _, err := connection.Client.GetUserMacros(info.UID); err != nil {
			t.Fatal("failed to make call after logging in second user via token: ", err)
		}

	})

}

func TestMFA(t *testing.T) {
	// spawn a test client
	// connect to the server for manual calls
	/*testclient, err := grav.NewOpts(grav.Opts{Server: server, UseHttps: false, InsecureNoEnforceCerts: true})
	if err != nil {
		t.Fatal(err)
	}

	lr, err := testclient.LoginEx(defaultUser, defaultPass)

	lr, err := testclient.MFALogin(defaultUser, defaultPass, types.AUTH_TYPE_NONE, "")
	t.Log(lr)
	t.Log(err)

	mfa, err := testclient.GetMFAInfo()
	t.Log(mfa)
	t.Log(err)
	t.Fail() */

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

// Creates a second account using via the logged-in test client.
// If MFA, a TOTP is added to the new user.
//
// Fatal on failure.
func createAltUser(t *testing.T, testclient *grav.Client, mfa bool) {
	if _, err := testclient.LookupUser(altUser); err != nil { // check if the user already exists (such as from running this test multiple times)
		t.Logf("failed to lookup user %v, attempting creation...", altUser)
		if err := testclient.AddUser(altUser, altPass, "Mildred Knolastname", "milly@imp.com", false); err != nil {
			t.Fatal(err)
		}

		if mfa {
			// TODO
			// initialize TOTP
			//testclient.GetTOTPSetupEx()
			//testclient.InstallTOTPSetup()
		}
	}
}

func deleteAltUser(t *testing.T, testclient *grav.Client) {
	u, err := testclient.LookupUser(altUser)
	if err != nil { // check if the user already exists (such as from running this test multiple times)
		t.Logf("failed to lookup user %v, skipping deletion.", altUser)
		return
	}

	if err := testclient.DeleteUser(u.UID); err != nil {
		t.Fatal(err)
	}
}
