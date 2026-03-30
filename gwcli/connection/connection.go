/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package connection implements and controls a Singleton instantiation of the gravwell client library.
All calls to the Gravwell instances should be called via this package and the client it controls.

Login logic is handled here and roughly follows this flow:

```mermaid
flowchart TB

	%% entry points
	APIToken["User provides API token<br>**--api**"]
	~~~
	bothCred["User provides username and password/passfile<br> **-u <> {-p <>| --password <>}**"]
	~~~
	noCred["User provides no credentials"]

	%% shared
	generateJWT["Generate local JWT"]
	success(("successful<br>login"))
	~~~
	fail(("fail out"))
	MFAPrompt["prompt for TOTP or recovery"]

	generateJWT --> success

	%% api token
	validateAPIToken{"API token is valid"}

	APIToken --> validateAPIToken --"yes"----> success
	validateAPIToken --"no"--> ErrInvalidAPI

	%% both credentials
	bcScript{"**--script**"}
	bcMFA{"MFA required"}

	bothCred --> bcMFA --"yes"--> bcScript --"yes"--> ErrAPITokenReq
	bcMFA --"no"--> generateJWT
	bcScript --"no"--> MFAPrompt --> generateJWT

	%% no cred
	ncMFA{"MFA required"}
	ncJWT{"does a valid<br>token exist?"}
	ncScript{"**--script**"}
	ncPromptCred["prompt for credentials"]
	ncScriptPostJWT{"**--script**"}

	noCred --> ncMFA
	ncMFA --"yes"--> ncScript --"yes"--> ErrAPITokenReq
	                 ncScript --"no"--> MFAPrompt
	ncMFA --"no"--> ncJWT --"yes"--> success
	                ncJWT --"no"--> ncScriptPostJWT
	ncScriptPostJWT --"yes"--> ErrCredOrAPIKeyReq
	ncScriptPostJWT --"no"--> ncPromptCred --> MFAPrompt



	%% Errors
	ErrAPITokenReq(["*stderr*:<br>MFA is enabled, API token is required"]) --> fail
	ErrInvalidAPI(["*stderr*:<br>API token is invalid"]) --> fail
	ErrCredOrAPIKeyReq(["*stderr*:<br>Credentials or API token required"]) --> fail

```

This package also contains some wrapper functions for grav.Client calls where we want to ensure consistent access and parameters.
*/
package connection

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection/credprompt"
	"github.com/gravwell/gravwell/v4/gwcli/connection/mfaprompt"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/objlog"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

const (
	jwtPermissions os.FileMode = 0600
)

type ErrBadPermissions struct {
	Expected os.FileMode
	Actual   os.FileMode
}

func (e ErrBadPermissions) Error() string {
	return fmt.Sprintf("incorrect permissions. Should be %[1]s(%[1]o), got %[2]s(%[2]o)", e.Expected, e.Actual)
}

func (e ErrBadPermissions) Is(err error) bool {
	_, ok := err.(ErrBadPermissions)
	return ok
}

// Client is the primary connection point from GWCLI to the gravwell backend.
var (
	clientMu sync.Mutex // should be held when making changes to the local Client instance
	Client   *grav.Client
	// MyInfo holds cached data about the current user.
	myInfo types.User
)

const refreshBuffer time.Duration = 5 * time.Minute // the refresher will next wake at (expiryTime - buffer)
var refresherDone chan bool                         // true is sent when the connection is closing, thereby alerting the current refresher to wrap up

// Initialize creates and starts a Client using the given connection string of the form <host>:<port>.
// Destroys a pre-existing connection (but does not log out), if there was one.
// restLogPath should be left empty outside of test packages.
//
// You probably want to call Login after a successful Initialize call.
func Initialize(conn string, UseHttps, InsecureNoEnforceCerts bool, restLogPath string) (err error) {
	clientMu.Lock()
	defer clientMu.Unlock()
	if Client != nil {
		if err := end(); err != nil {
			clilog.Writer.Warnf("failed to end Client: %v", err)
		}
	}

	var l objlog.ObjLog = nil
	if restLogPath != "" { // used for testing, not intended for production modes
		l, err = newRestRotator(restLogPath)
		if err != nil {
			return err
		}
	} else if clilog.Writer != nil && (clilog.Writer.GetLevel() == log.Level(clilog.DEBUG) || clilog.Writer.GetLevel() == log.Level(clilog.INFO)) { // spin up the rest logger if in INFO+
		l, err = newRestRotator(cfgdir.DefaultRestLogPath)
		if err != nil {
			return err
		}
	}

	if Client, err = grav.NewOpts(
		grav.Opts{
			Server:                 conn,
			UseHttps:               UseHttps,
			InsecureNoEnforceCerts: InsecureNoEnforceCerts,
			ObjLogger:              l,
		}); err != nil {
		return err
	}

	return nil
}

// Login the Initialize()'d client.
// On success, caches user info and generates a JWT for use in future logins.
//
// Ineffectual if Client is already logged in.
//
// Has 3, distinct modes (in order of priority):
//
// 1. API token. Interactive+Script; unaffected by MFA.
//
// 2. Username/Password. Interactive+~Script; prompts for MFA if enabled for the user.
// Fails out instead of prompting in script mode.
//
// 3. None or only username. Attempts to login via JWT. Prompts for u/p if JWT fails.
// Fails out instead of prompting in script mode.
//
// Logs the method the user logged in if successful, otherwise returns an error.
func Login(username string, password, apiToken *string, noInteractive bool) error {
	clientMu.Lock()
	defer clientMu.Unlock()
	if Client == nil {
		return ErrNotInitialized
	}
	if Client.LoggedIn() {
		return nil
	}

	// set on success so it can be logged
	var method = "unknown"
	if apiToken != nil && *apiToken != "" { // api token
		if err := Client.LoginWithAPIToken(*apiToken); err != nil {
			return errors.Join(ErrAPITokenInvalid, err)
		}
		method = "API_token"
	} else if username != "" && (password != nil && *password != "") { // u/p
		if err := loginWithCredentials(username, *password, noInteractive); err != nil {
			return err
		}
		method = "explicit_username_password"
	} else { // no credentials or only a username
		// check the JWT
		if err := loginViaJWT(username); err != nil {
			clilog.Writer.Warnf("failed to login via JWT: %v", err)
			if errors.Is(err, ErrBadPermissions{}) {
				fmt.Fprintf(os.Stderr, "Your login token has incorrect permissions and was ignored. Expected %[1]s(%[1]o)\n", jwtPermissions)
			}
			// failing to login via JWT is non-fatal in interactive mode
			if noInteractive {
				return ErrAPITokenRequired
			}
			if mfa, err := promptForMissingCredentials(username); err != nil {
				return err
			} else if mfa {
				method = "prompt+mfa"
			} else {
				method = "prompt"
			}
		} else {
			method = "JWT"
		}
	}
	clilog.Writer.Info("login successful", rfc5424.SDParam{Name: "method", Value: method})
	// if we made it this far, we have successfully logged in via one of the above branches

	// on successful login, fetch and cache MyInfo
	var err error
	if myInfo, err = Client.MyInfo(); err != nil {
		return errors.New("failed to cache user info: " + err.Error())
	}

	// check that the info of the user we fetched actually matches the given username
	if username != "" && myInfo.Username != username {
		return fmt.Errorf("server returned a different username (%v) than the given credentials (%v)", myInfo.Username, username)
	}

	// create/refresh the token
	if err := writeOutJWT(myInfo.Username); err != nil {
		clilog.Writer.Warnf("%v", err.Error())
		// failing to create the token is not fatal
	}
	refresherDone = make(chan bool)
	go keepJWTRefreshed(refresherDone)

	// while most login methods call Sync for us, JWT does not.
	// To ensure the data exists no matter what changes occur or which method we use, Sync now.
	return Client.Sync()
}

// helper function for Login when BOTH credentials were explicitly set.
//
// Fails if noInteractive && mfa required
//
// If error is nil, caller can assume Client has successfully logged in and state has been logged (if applicable).
func loginWithCredentials(username, password string, noInteractive bool) error {
	resp, err := Client.LoginEx(username, password)
	if mfa, ufErr := testLoginError(resp, err); ufErr != nil {
		return ufErr
	} else if mfa {
		// if we are in script mode, fail out and alert the user to use an API key
		if noInteractive {
			return ErrAPITokenRequired
		}

		// send the user into a prompt to enter their TOTP
		code, authType, err := mfaprompt.Collect()
		if err != nil {
			return err
		}
		resp, err = Client.MFALogin(username, password, authType, code)
		if err != nil {
			return err
		} else if !resp.LoginStatus {
			// we logged in via MFA, didn't get an error, but still failed to actually log in
			clilog.Writer.Criticalf("failed to login, unknown response state: %+v", resp)
			return uniques.ErrGeneric
		}
	}

	return nil
}

// loginViaJWT attempts to login via JWT token in the user's config directory.
// If the token is malformed in anyway, it is considered invalid.
//
// Returns an error on failures.
// This error should be considered nonfatal and the user logged in via an alternative method instead.
//
// If a username was given, it will first be matched against the username found in the file.
// NOTE(rlandau): we still perform a whois against the backend later, but this allows us a sanity check without touching the backend.
func loginViaJWT(username string) (err error) {
	var tknbytes []byte
	// NOTE the reversal of standard error checking (`err == nil`)
	if fi, err := os.Stat(cfgdir.DefaultTokenPath); err != nil {
		return err
	} else { // sanity check the file
		mode := fi.Mode()
		if mode.IsDir() {
			return errors.New("login token must be a file")
		}
		if mode != jwtPermissions {
			return ErrBadPermissions{jwtPermissions, mode}
		}
	}
	if tknbytes, err = os.ReadFile(cfgdir.DefaultTokenPath); err == nil {
		// split the username and token
		exploded := strings.Split(string(tknbytes), "\n")
		if len(exploded) != 2 || exploded[0] == "" || exploded[1] == "" {
			return errors.New("failed to split token file into <username>\n<token>")
		}
		if (username != "") && username != exploded[0] {
			return fmt.Errorf("tokenfile username (%v) does not match given username (%v)", exploded[0], username)
		}

		if err = Client.ImportLoginToken(string(exploded[1])); err == nil {
			if err = Client.TestLogin(); err == nil {
				return nil
			}
		}
	}
	return
}

// Spins up a bubble tea prompt to interactively collect u/p and another to collect MFA (if applicable).
// Returns if the MFA prompt was displayed and filled out (if !mfa, the Client successfully auth'd without MFA)
// Only prints to the log on critical failures
//
// ! Not to be called in script mode.
func promptForMissingCredentials(prepopUsername string) (mfa bool, err error) {
	// prompt for user name and password
	u, p, err := credprompt.Collect(prepopUsername)
	if err != nil {
		return false, err
	}

	// log in via u/p
	resp, err := Client.LoginEx(u, p)
	if mfa, ufErr := testLoginError(resp, err); ufErr != nil {
		return false, ufErr
	} else if mfa {
		// prompt for TOTP or recovery code
		code, authType, err := mfaprompt.Collect()
		if err != nil {
			return true, err
		}
		resp, err = Client.MFALogin(u, p, authType, code)
		if err != nil {
			return true, err
		} else if !resp.LoginStatus {
			// we logged in via MFA, didn't get an error, but still failed to actually log in
			clilog.Writer.Criticalf("failed to login, unknown response state: %+v", resp)
			return false, uniques.ErrGeneric
		}
	}

	return mfa, nil

}

// helper subroutine that wraps LoginEx.
// Translates HTTP error codes into user-friendly errors and does not treat MFARequired as an error.
// Logs the full error to the log and returns an error appropriate to show a user.
//
// Really just used to consolidate all of the checks that are made each time we would call LoginEx().
func testLoginError(resp types.LoginResponse, rawErr error) (mfa bool, userFriendlyErr error) {
	if rawErr == nil {
		if !resp.LoginStatus { // sanity check
			clilog.Writer.Criticalf("login did not turn back an error, but we are not logged in! Response: %v", resp)
			return false, uniques.ErrGeneric
		}

		return false, nil
	}

	// no need to handle these errors, just pass them forward
	if errors.Is(rawErr, grav.ErrLoginFail) ||
		errors.Is(rawErr, grav.ErrAccountLocked) {
		return false, rawErr
	} else if errors.Is(rawErr, grav.ErrMFARequired) {
		// sanity checks
		if resp.MFASetupRequired {
			return false, ErrMFASetupRequired // local error has better readability than grav.ErrMFASetupRequired
		} else if !resp.MFARequired {
			// we aren't logged in, but it isn't because MFARequired
			// unknown state, fail out
			clilog.Writer.Criticalf("failed to login, unknown response state: %+v", resp)
			return false, uniques.ErrGeneric
		}

		return true, nil // fetch MFA from the user
	}

	// unhandled error states
	clilog.Writer.Errorf("an unhandled error occurred during login: %v", rawErr)
	return false, rawErr
}

// writeOutJWT writes a login token (JWT) to the default path for easier future logins.
//
// Token files have the form:
//
// <username>
//
// <token>
func writeOutJWT(username string) error {
	var (
		err   error
		token string
	)
	if token, err = Client.ExportLoginToken(); err != nil {
		return fmt.Errorf("failed to export login token: %v", err)
	}

	// write out the username, then the token
	fd, err := os.OpenFile(cfgdir.DefaultTokenPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, jwtPermissions)
	if err != nil {
		return fmt.Errorf("failed to create token: %v", err)
	}

	if err := fd.Chmod(jwtPermissions); err != nil { // ensure permissions are correct
		return fmt.Errorf("failed to set token permissions: %v", err)
	}

	if _, err := fd.WriteString(username + "\n" + token); err != nil {
		return fmt.Errorf("failed to write token: %v", err)
	}

	if err := fd.Sync(); err != nil {
		return fmt.Errorf("failed to sync token file: %v", err)
	}
	if err = fd.Close(); err != nil {
		return fmt.Errorf("failed to close token file: %v", err)
	}

	clilog.Writer.Info("created token file",
		rfc5424.SDParam{Name: "user", Value: username},
		rfc5424.SDParam{Name: "path", Value: cfgdir.DefaultTokenPath})
	return nil
}

// keepJWTRefreshed automatically refreshes Client and the login JWT every so often.
// Intended to be called in a goroutine, keepJWTRefreshed parses the token for when it expires, sleeps until a short time before it expires, then refreshes it.
func keepJWTRefreshed(kill chan bool) {
	for {
		clientMu.Lock()
		var wakeAt = getJWTExpiry()
		var sleepTime = time.Until(wakeAt)
		clientMu.Unlock()

		clilog.Writer.Debugf("waking at @ %v (sleeping for %v)", wakeAt, sleepTime)

		select {
		case <-kill:
			clilog.Writer.Debug("closing up shop", rfc5424.SDParam{Name: "sublogger", Value: "refresher"})
			return
		case <-time.After(sleepTime):
			clientMu.Lock()
			// ensure the client is still in an acceptable state
			if Client == nil {
				// client has been killed, but we haven't received the signal yet
				clientMu.Unlock()
				continue
			} else if Client.State() != grav.STATE_AUTHED {
				clientMu.Unlock()
				clilog.Writer.Error("failed to refresh login: client not authenticated", rfc5424.SDParam{Name: "sublogger", Value: "refresher"})
				// back off for a few minutes
				time.Sleep(3 * time.Minute)
				continue
			}
			// token will expire soon, regenerate it
			if err := Client.RefreshLoginToken(); err != nil {
				clilog.Writer.Error("failed to refresh login", rfc5424.SDParam{Name: "sublogger", Value: "refresher"}, rfc5424.SDParam{Name: "Error", Value: err.Error()})
			}
			// write the new token to our token file
			clilog.Writer.Info("rewriting token file ",
				rfc5424.SDParam{Name: "username", Value: myInfo.Name},
				rfc5424.SDParam{Name: "path", Value: cfgdir.DefaultTokenPath},
				rfc5424.SDParam{Name: "sublogger", Value: "refresher"})
			if err := writeOutJWT(myInfo.Username); err != nil {
				clilog.Writer.Warnf("%v", err)
			}
			clientMu.Unlock()
		}
	}
}

// helper function for keepRefreshed.
// Slurps the token file, returning when the caller should awaken to refresh this token (with a built-in buffer).
// wakeTime will be time.Now() if an error occurred.
func getJWTExpiry() (wakeTime time.Time) {
	tkn, err := os.ReadFile(cfgdir.DefaultTokenPath)
	if err != nil {
		// log the error and return
		clilog.Writer.Warnf("refresher: failed to read existing JWT: %v", err)
		return time.Now()
	}

	// skim off username
	exploded := strings.Split(string(tkn), "\n")
	if myInfo.Username != exploded[0] {
		// either the token or the local cache has changed
		clilog.Writer.Infof("connection username %v does not match token username %v", myInfo.Username, exploded[0])
		return time.Now()
	}

	_, payload, _, err := uniques.ParseJWT(exploded[1])
	if err != nil {
		clilog.Writer.Warnf("failed to parse JWT: %v", err)
		return time.Now()
	}

	return payload.Expires.Add(-refreshBuffer)

}

// CurrentUser returns the local cache of information about the currently logged-in user.
// Returns the zero value if the local client is not authenticated.
func CurrentUser() types.User {
	clientMu.Lock()
	defer clientMu.Unlock()

	if Client.State() != grav.STATE_AUTHED {
		return types.User{}
	}

	return myInfo
}

// End closes the connection to the server and destroys the data in the connection singleton.
// Does not logout the user as to not invalidate existing JWTs.
//
// To reconnect, you will need to call Initialize() again.
//
// ! swallows Already Closed errors
func End() error {
	clientMu.Lock()
	defer clientMu.Unlock()
	return end()
}

// internal, lock-less implementation of End.
func end() error {
	myInfo = types.User{}
	if Client == nil { // job's done
		return nil
	} else if Client.State() == grav.STATE_CLOSED || Client.State() == grav.STATE_LOGGED_OFF {
		return nil
	}

	// alert the JWT refresher to shutdown
	// if we are authed, a refresher should be running that must be stopped
	if refresherDone != nil {
		close(refresherDone)
		refresherDone = nil
	}

	if err := Client.Close(); err != nil && (err.Error() != "Client already closed") {
		return err
	}

	//Client = nil // does not nil out as to reduce the likelihood of nil pointer panics

	return nil
}

// CreateScheduledSearch is a validation wrapper around Client.CreateScheduledSearch to provide consistent
// validation, logging, and errors.
//
// Returns:
//   - an ID on success, -1 on failure
//   - a reason on invalid parameters
//   - and an error iff the server returns an error
func CreateScheduledSearch(name, desc, freq, qry string, dur time.Duration) (
	id string, invalid string, err error,
) {
	// validate parameters
	if qry == "" {
		return id, "cannot schedule an empty query", nil
	} else if name == "" || freq == "" {
		return id, "name and frequency are required", nil
	} else if dur < 0 {
		return id, fmt.Sprintf("duration must be positive (given:%v)", dur), nil
	}

	exploded := strings.Split(freq, " ")
	// validate cron format (`0-59` `0-23` `1-31` `1-12` `0-7`, ranges inclusive)
	if len(exploded) != 5 {
		return id, "frequency must have 5 elements, in the format '* * * * *'", nil
	}
	if inv := invalidCronWord(exploded[0], "minute", 0, 59); inv != "" {
		return id, inv, nil
	}
	if inv := invalidCronWord(exploded[1], "hour", 0, 23); inv != "" {
		return id, inv, nil
	}
	if inv := invalidCronWord(exploded[2], "day of the month", 1, 31); inv != "" {
		return id, inv, nil
	}
	if inv := invalidCronWord(exploded[3], "month", 1, 12); inv != "" {
		return id, inv, nil
	}
	if inv := invalidCronWord(exploded[4], "day of the week", 0, 6); inv != "" {
		return id, inv, nil
	}

	// submit the request
	clilog.Writer.Debugf("Scheduling query %v (%v) for %v", name, qry, freq)
	// TODO provide a dialogue for selecting groups/permissions
	spec := types.ScheduledSearch{
		CommonFields: types.CommonFields{
			Name:        name,
			Description: desc,
		},
		AutomationCommonFields: types.AutomationCommonFields{
			Schedule: freq,
		},
		SearchString: qry,
		Duration:     int64(dur.Seconds()),
	}
	var result types.ScheduledSearch
	result, err = Client.CreateScheduledSearch(spec)
	if err != nil {
		return "", "", fmt.Errorf("failed to schedule search: %v", err)
	}
	return result.ID, "", nil
}

// Validates the given cron word, ensuring it parses and is between the two bounds (inclusively).
// entryNumber is the order of this word ("first", "second", "third", ...).
func invalidCronWord(word, idxDescriptor string, lowBound, highBound int) (invalid string) {
	if i, err := strconv.Atoi(word); err != nil {
		// check for astrisk
		if runes := []rune(word); len(runes) == 1 && runes[0] == '*' {
			return ""
		}
		return "failed to parse " + word
	} else if i < lowBound || i > highBound {
		return fmt.Sprintf("%s must be between %d and %d, inclusively",
			idxDescriptor, lowBound, highBound)
	}
	return ""
}

// StartQuery validates and submits the given query to the connected server instance.
// Duration must be negative or zero (X time units back in time from now()).
// A positive duration will result in an error.
//
// Returns a handle to executing searching.
func StartQuery(qry string, durFromNow time.Duration, background bool) (grav.Search, error) {
	var err error
	if durFromNow > 0 {
		return grav.Search{}, fmt.Errorf("duration must be negative or zero (given %v)", durFromNow)
	}

	// validate search query
	// TODO do not re-validate the query
	if err = Client.ParseSearch(qry); err != nil {
		return grav.Search{}, fmt.Errorf("'%s' is not a valid query: %s", qry, err.Error())
	}

	// check for scheduling

	end := time.Now()
	sreq := types.StartSearchRequest{
		SearchStart:  end.Add(durFromNow).Format(uniques.SearchTimeFormat),
		SearchEnd:    end.Format(uniques.SearchTimeFormat),
		Background:   background,
		SearchString: qry, // pull query from the commandline
		NoHistory:    false,
		Preview:      false,
	}
	var fgbg = "foreground"
	if background {
		fgbg = "background"
	}
	s, err := Client.StartSearchEx(sreq)
	clilog.Writer.Infof("Executed %v search '%v' (id: %s) from %v -> %v",
		fgbg, sreq.SearchString, s.ID, sreq.SearchStart, sreq.SearchEnd)
	return s, err

}

// DownloadQuerySuccessfulString returns a consistent sting for a successful query result download
func DownloadQuerySuccessfulString(filename string, append bool, format string) string {
	var word = "wrote"
	if append {
		word = "appended"
	}
	return fmt.Sprintf("Successfully %v %v results to %v", word, format, filename)
}

// GetResultsForWriter waits on and downloads the given results according to their associated render type
// (JSON, CSV, if given, otherwise the normal form of the results),
// returning an io.ReadCloser to stream the results and the format they are in.
// If a TimeRange is given, only results in that timeframe will be included.
//
// This should be used to get results when they will be written ton io.Writer (a file or stdout).
//
// This call blocks until the search is completed.
//
// Typically called prior to PutResultsToWriter.
func GetResultsForWriter(s *grav.Search, tr types.TimeRange, csv, json bool) (rc io.ReadCloser, format string, err error) {
	if err := Client.WaitForSearch(*s); err != nil {
		return nil, "", err
	}

	// determine the format to request results in
	if json {
		format = types.DownloadJSON
	} else if csv {
		format = types.DownloadCSV
	} else {
		switch s.RenderMod {
		case types.RenderNameHex, types.RenderNameRaw, types.RenderNameText:
			format = types.DownloadText
		case types.RenderNamePcap:
			format = types.DownloadPCAP
		default:
			format = types.DownloadArchive
		}
	}
	clilog.Writer.Infof("renderer '%s' -> '%s'", s.RenderMod, format)

	// fetch and return results
	rc, err = Client.DownloadSearch(s.ID, tr, format)
	return rc, format, err
}

// RefreshCurrentUser force-updates the local cache of user information.
func RefreshCurrentUser() error {
	clientMu.Lock()
	defer clientMu.Unlock()

	mi, err := Client.MyInfo()
	if err != nil {
		return err
	}
	myInfo = mi

	return nil
}

//#region super functions
// This region covers functions that wrap/bolster Client functionality.
// Typically only necessary for special purposes (like AdminMode returning false if the connection DNE)

func AdminMode() bool {
	if Client == nil {
		return false
	}
	return Client.AdminMode()
}

//#endregion super functions
