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
	"errors"
	"io/fs"
	"os"
	"path"
	"testing"
	"time"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/objlog"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
	"github.com/pquerna/otp/totp"
)

const (
	server = "localhost:80"
	// default user
	defaultUser       string        = "admin"
	defaultPass       string        = "changeme"
	apiTokenExpiryDur time.Duration = time.Minute
	// second user, created and deleted between tests
	altUser string = "Milly"
	altPass string = "LooLooLand"
	tempkey string = "zSaCgH-0sh3pd8CNwLCc9k-3N2BnAczAW1x6yedQUgCGd8b4xIbNtZQNx2-PjF8"
)

// TestLoginNoMFA tests all --script entrypoints to logging in.
// NOTE: this test suite assumes that the default user does NOT have MFA enabled and can be accessed via u/p.
func TestLoginNoMFA(t *testing.T) {
	// setup singletons
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "DEBUG"); err != nil {
		t.Fatalf("%v", err)
	}
	if err := connection.Initialize(server, false, true, path.Join(t.TempDir(), "rest.log")); err != nil {
		panic(err)
	}

	// spawn a test client
	testclient, err := grav.NewOpts(grav.Opts{Server: server, UseHttps: false, InsecureNoEnforceCerts: true, ObjLogger: &objlog.NilObjLogger{}})
	if err != nil {
		t.Skip("failed to create test client for fetching API token: ", err)
	}
	if resp, err := testclient.LoginEx(defaultUser, defaultPass); err != nil {
		t.Skip(err)
	} else if !resp.LoginStatus {
		t.Skip("failed to log test client in: ", resp.Reason)
	}
	APITkn := generateAPIToken(t, testclient)

	type args struct {
		u          string
		p          string
		apiToken   string
		scriptMode bool
	}
	tests := []struct {
		name        string
		args        args
		expectedErr error
	}{
		{"script mode: valid username and password", args{defaultUser, defaultPass, "", true}, nil},
		{"script mode: valid APIToken", args{"", "", APITkn, true}, nil},
		{"script mode: no credentials", args{"", "", "", true}, connection.ErrCredentialsOrAPITokenRequired},
		{"script mode: invalid password", args{defaultUser, "badpassword", "", true}, connection.ErrInvalidCredentials},
		{"script mode: invalid APIToken", args{"", "", APITkn + "1234", true}, connection.ErrAPIKeyInvalid},
		{"script mode: only username", args{defaultUser, "", "", true}, connection.ErrCredentialsOrAPITokenRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.args.apiToken == "UNSET" {
				t.Skip("missing API token; skipping...")
			}

			// re-initialize the connection singleton
			if err := connection.Initialize(server, false, true, ""); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { connection.End() })

			// ensure there is no cached JWT
			if err := os.Remove(cfgdir.DefaultTokenPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				t.Fatal(err)
			}

			// attempt to authenticate
			if err := connection.Login(tt.args.u, tt.args.p, tt.args.apiToken, tt.args.scriptMode); !errors.Is(err, tt.expectedErr) {
				t.Fatalf("Login() error = '%v', want = '%v'", err, tt.expectedErr)
			} else if err == nil {
				// additional checks to perform if we were not expected and did not receive an error

				// check that Client is ready to go
				if connection.Client == nil {
					t.Fatal("client is nil")
				}

				// check that we can query the backend and get the correct user
				myinfo, err := connection.Client.MyInfo()
				if err != nil {
					t.Fatal(err)
				} else if myinfo.User != connection.MyInfo.User || (tt.args.u != "" && myinfo.User != tt.args.u) {
					t.Fatalf("username mismatch! query name (%v) != cached name (%v) != argument username (%v)", myinfo.User, connection.MyInfo.User, tt.args.u)
				}
			}

		})
	}

	t.Run("u/p login -> token login -> different u/p login", func(t *testing.T) {
		// spin up a test client
		defaultClient, err := grav.NewOpts(grav.Opts{Server: server, UseHttps: false, InsecureNoEnforceCerts: true})
		if err != nil {
			t.Fatal(err)
		}
		if err = defaultClient.Login(defaultUser, defaultPass); err != nil {
			t.Fatal(err)
		}

		// create a second user
		userExistedPrior, _ := createAltUser(t, defaultClient, false)
		if userExistedPrior {
			deleteAltUser(t, defaultClient)
			if userExistedPrior, _ = createAltUser(t, defaultClient, false); userExistedPrior {
				t.Fatal("alt user already existed despite explicit deletion")
			}

		}
		t.Cleanup(func() { deleteAltUser(t, defaultClient) })

		// destroy the Client singleton between each test
		if connection.Client != nil {
			connection.Client.Logout()
		}

		// reinitialize the client
		if err := connection.Initialize(server, false, true, path.Join(t.TempDir(), "rest.log")); err != nil {
			t.Fatal(err)
		}

		// ensure no token exists
		if err := os.Remove(cfgdir.DefaultTokenPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
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

// TestLoginMFA runs subtests similar to TestLoginNoMFA, but includes a user with MFA enabled.
func TestLoginMFA(t *testing.T) {
	// set up logger
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "DEBUG"); err != nil {
		t.Fatalf("%v", err)
	}

	// spin up test client
	defaultClient, err := grav.NewOpts(grav.Opts{Server: server, UseHttps: false, InsecureNoEnforceCerts: true, ObjLogger: &objlog.NilObjLogger{}})
	if err != nil {
		t.Skip("failed to create test client for fetching API token: ", err)
	}
	if resp, err := defaultClient.LoginEx(defaultUser, defaultPass); err != nil {
		t.Skip(err)
	} else if !resp.LoginStatus {
		t.Skip("failed to log test client in: ", resp.Reason)
	}
	t.Cleanup(func() { defaultClient.Logout() })
	// fetch an API token for the default user
	//defaultAPITkn, defaultUAPITknSuccess := generateAPIToken(t, defaultClient)

	// create a second account with MFA so we don't screw up admin
	userExistedPrior, altTOTPSecret := createAltUser(t, defaultClient, true)
	if userExistedPrior || altTOTPSecret == "" {
		deleteAltUser(t, defaultClient)
		if userExistedPrior, altTOTPSecret = createAltUser(t, defaultClient, true); userExistedPrior {
			t.Fatal("alt user already existed despite explicit deletion")
		}

	}
	t.Cleanup(func() { deleteAltUser(t, defaultClient) })

	// spin up a client for the alt user
	altClient, err := grav.NewOpts(grav.Opts{Server: server, UseHttps: false, InsecureNoEnforceCerts: true, ObjLogger: &objlog.NilObjLogger{}})
	if err != nil {
		t.Skip("failed to create test client for fetching API token: ", err)
	}
	t.Cleanup(func() { altClient.Logout() })
	code, err := totp.GenerateCode(altTOTPSecret, time.Now())
	if err != nil {
		t.Fatal("failed to generate TOTP code: ", err)
	}
	if resp, err := altClient.MFALogin(altUser, altPass, types.AUTH_TYPE_TOTP, code); err != nil {
		t.Skip(err)
	} else if !resp.LoginStatus {
		t.Skip("failed to log alt client in: ", resp.Reason)
	}
	t.Cleanup(func() { defaultClient.Logout() })

	// fetch an API token for the second user
	altAPITkn := generateAPIToken(t, altClient)

	type args struct {
		u          string
		p          string
		apiToken   string
		scriptMode bool
	}
	tests := []struct {
		name        string
		args        args
		expectedErr error
	}{
		{"script mode: (alt user) valid username and password, MFA enabled", args{altUser, altPass, "", true}, connection.ErrAPITokenRequired},
		{"script mode: (alt user) valid APIToken", args{"", "", altAPITkn, true}, nil},
		{"script mode: (alt user) no credentials", args{"", "", "", true}, connection.ErrCredentialsOrAPITokenRequired},
		{"script mode: (alt user) invalid password", args{defaultUser, "badpassword", "", true}, connection.ErrInvalidCredentials},
		{"script mode: (alt user) invalid APIToken", args{"", "", altAPITkn + "1234", true}, connection.ErrAPIKeyInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// if we were given an API key, but failed to create one, skip the test
			if tt.args.apiToken == "UNSET" {
				t.Skip("missing API token; skipping...")
			}

			// re-initialize the connection singleton
			if err := connection.Initialize(server, false, true, ""); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { connection.End() })

			// ensure there is no cached JWT
			if err := os.Remove(cfgdir.DefaultTokenPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				t.Fatal(err)
			}

			// attempt to authenticate
			if err := connection.Login(tt.args.u, tt.args.p, tt.args.apiToken, tt.args.scriptMode); !errors.Is(err, tt.expectedErr) {
				t.Fatalf("Login() error = '%v', want = '%v'", err, tt.expectedErr)
			} else if err == nil {
				// additional checks to perform if we were not expected and did not receive an error

				// check that Client is ready to go
				if connection.Client == nil {
					t.Fatal("client is nil")
				}

				// check that we can query the backend and get the correct user
				myinfo, err := connection.Client.MyInfo()
				if err != nil {
					t.Fatal(err)
				} else if myinfo.User != connection.MyInfo.User || (tt.args.u != "" && myinfo.User != tt.args.u) {
					t.Fatalf("username mismatch! query name (%v) != cached name (%v) != argument username (%v)", myinfo.User, connection.MyInfo.User, tt.args.u)
				}
			}

		})
	}

}

// Creates an API token with "ListUsers", "ListGroups", "ListGroupMembers" capabilities for the logged-in testclient.
// Returns the token that was generated (to be passed into connection.Login()).
// On failure, tkn will default to "UNSET".
// If successful, queues a Cleanup function to delete the token.
func generateAPIToken(t *testing.T, testclient *grav.Client) (tkn string) {
	const tknfailVal string = "UNSET"
	tf, err := testclient.CreateToken(
		types.TokenCreate{
			Name:         "LoginMFAToken",
			Desc:         "API token for the LoginMFA tests",
			Expires:      time.Now().Add(apiTokenExpiryDur),
			Capabilities: []string{"ListUsers", "ListGroups", "ListGroupMembers"}})
	if err != nil {
		t.Log("failed to generate APIKey, skipping tests: ", err)
		return "UNSET"
	}
	t.Cleanup(func() { testclient.DeleteToken(tf.ID) })

	return tf.Value
}

// Initializes and logs, calling fatal on the first error
func initLogin(t *testing.T, u, p string) {
	if err := connection.Initialize(server, false, true, path.Join(t.TempDir(), "rest.log")); err != nil {
		t.Fatal(err)
	}

	if err := connection.Login(u, p, "", true); err != nil {
		t.Fatal(err)
	}
}

// Creates a second account using via the logged-in test client.
// If MFA, a TOTP is added to the new user and the secret for generating codes is returned.
//
// Fatal on error, but if the user already exists that will be returned as true and no action will be taken.
// The TOTP secret is only returned returned if mfa and the new user is actually created.
func createAltUser(t *testing.T, testclient *grav.Client, mfa bool) (userExistedPrior bool, altUserTOTPSecret string) {
	if _, err := testclient.LookupUser(altUser); err != nil { // check if the user already exists (such as from running this test multiple times)
		t.Logf("failed to lookup user %v, attempting creation...", altUser)
		if err := testclient.AddUser(altUser, altPass, "Mildred Knolastname", "milly@imp.com", false); err != nil {
			t.Fatal(err)
		}

		if mfa {
			// initialize TOTP
			sr, err := testclient.GetTOTPSetupEx(altUser, altPass, types.AUTH_TYPE_NONE, "")
			if err != nil {
				t.Fatal(err)
			}

			// generate a code to confirm TOTP installation
			code, err := totp.GenerateCode(sr.Seed, time.Now())
			if err != nil {
				t.Fatal("failed to generate TOTP code from setup seed: ", err)
			}
			t.Logf("generated totp code '%v'", code)

			_, err = testclient.InstallTOTPSetup(altUser, altPass, code)
			if err != nil {
				t.Fatal(err)
			}
			//t.Log(ir)
			return false, sr.Seed
		}
		return false, ""
	}
	return true, ""
}

// Destroys the secondary user, if it exists.
//
// Fatal on failure.
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
