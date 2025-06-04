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

Login logic is handled here with the following logical flow:

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
	"time"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/google/uuid"
	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/objlog"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

//const refreshInterval time.Duration = 10 * time.Minute // how often we refresh the user token

// Client is the primary connection point from GWCLI to the gravwell backend.
var Client *grav.Client

// MyInfo holds cached data about the current user.
var MyInfo types.UserDetails

// Initialize creates and starts a Client using the given connection string of the form <host>:<port>.
// Destroys a pre-existing connection (but does not log out), if there was one.
// restLogPath should be left empty outside of test packages.
//
// You probably want to call Login after a successful Initialize call.
func Initialize(conn string, UseHttps, InsecureNoEnforceCerts bool, restLogPath string) (err error) {
	if Client != nil {
		End()
	}

	var l objlog.ObjLog = nil
	if restLogPath != "" { // used for testing, not intended for production modes
		l, err = objlog.NewJSONLogger(restLogPath)
		if err != nil {
			return err
		}
	} else if clilog.Writer != nil && (clilog.Writer.GetLevel() == log.Level(clilog.DEBUG) || clilog.Writer.GetLevel() == log.Level(clilog.INFO)) { // spin up the rest logger if in INFO+
		l, err = objlog.NewJSONLogger(cfgdir.DefaultRestLogPath)
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

// Login the initialized Client.
// Attempts to use a JWT token first, then falls back to supplied credentials.
//
// Ineffectual if Client is already logged in.
func Login(username, password, apiToken string, scriptMode bool) (err error) {
	if Client == nil {
		return ErrNotInitialized
	}
	if Client.LoggedIn() {
		return nil
	}

	if apiToken != "" { // if an APIKey was given, attempt to login with it
		if err := Client.LoginWithAPIToken(apiToken); err != nil {
			return errors.Join(ErrAPIKeyInvalid, err)
		}
		clilog.Writer.Infof("logged in via API token")
	} else if username == "" { // if a username was not given, act as if no credentials were given
		err := loginNoCredentials(scriptMode)
		if err != nil {
			return err
		}
	} else if username != "" && password != "" {
		// if all credentials were given, try to log in using only those credentials
		if err := loginWithCredentials(username, password, scriptMode); err != nil {
			return err
		}
	} else { // a username was given, but no password/passfile
		// in script mode, fail out
		if scriptMode {
			return ErrCredentialsOrAPITokenRequired
		}
		// in interactive mode, throw up a prompt and pre-populate username
		mfa, err := promptForInput(username)
		if err != nil {
			return err
		}
		if mfa {
			clilog.Writer.Infof("logged in via credentials (with mfa)")
		} else {
			clilog.Writer.Infof("logged in via credentials (without mfa)")
		}

	}

	// if we made it this far, we have successfully logged in via one of the above branches
	// TODO

	// on successful login, fetch and cache MyInfo
	if MyInfo, err = Client.MyInfo(); err != nil {
		return errors.New("failed to cache user info: " + err.Error())
	}

	// check that the info of the user we fetched actually matches the given username
	if username != "" && MyInfo.User != username {
		return fmt.Errorf("server returned a different username (%v) than the given credentials (%v)", MyInfo.User, username)
	}

	// create/refresh the token
	if err := createTokenFile(username); err != nil {
		clilog.Writer.Warnf("%v", err.Error())
		// failing to create the token is not fatal
	}

	return nil
}

// helper function for Login when no credentials were given.
func loginNoCredentials(scriptMode bool) (err error) {
	// attempt to login to whichever account was responsible for the pre-existing token
	if err := loginViaJWT(""); err != nil {
		clilog.Writer.Warnf("failed to login via JWT token: %v", err)
		// if we are in script mode, fail out
		if scriptMode {
			return ErrCredentialsOrAPITokenRequired
		}

		mfa, err := promptForInput("")
		if err != nil {
			return err
		}
		if mfa {
			clilog.Writer.Infof("logged in via credentials (with mfa)")
		} else {
			clilog.Writer.Infof("logged in via credentials (without mfa)")
		}
	} else { // successfully logged in with JWT
		clilog.Writer.Infof("logged in via JWT")

		// if we are in script mode and MFA would have been required, fail out
		// this is to enforce consistent script usage, lest the token expire mid-script
		if scriptMode {
			mfa, err := Client.GetMFAInfo()
			if err != nil {
				err = errors.Join(errors.New("failed to fetch mfa info after token login"), err)

				clilog.Writer.Warnf("%v", err)
				return err
			} else if mfa.MFARequired {
				clilog.Writer.Infof("failing out anyways due to JWT+script+MFARequired")

				return ErrAPITokenRequired
			}
		}
	}

	// success

	return nil

}

// helper function for Login when credentials were explicitly set.
// If error is nil, caller can assume Client has successfully logged in and state has been logged (if applicable).
func loginWithCredentials(username, password string, script bool) error {
	resp, err := Client.LoginEx(username, password)
	if err != nil {
		// coarsely check for invalid credentials
		if strings.Contains(err.Error(), "401") {
			clilog.Writer.Infof("Failure Reason: %v", resp.Reason)
			return ErrInvalidCredentials
		}
		return err
	} else if resp.LoginStatus {
		// successful login, no need to continue to continue to MFA
		clilog.Writer.Infof("logged in via credentials (without mfa)")
		return nil
	}

	// not yet logged in, likely due to required MFA
	if resp.MFASetupRequired {
		return ErrMFASetupRequired
	} else if !resp.MFARequired {
		// we aren't logged in, but it isn't because MFARequired
		// unknown state, fail out
		clilog.Writer.Criticalf("failed to login, unknown response state: %+v", resp)
		return uniques.ErrGeneric
	}

	// if we are in script mode, fail out and alert the user to use an API key
	if script {
		return ErrAPITokenRequired
	}

	// send the user into a prompt to enter their TOTP
	code, authType, err := mfaPrompt()
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
	return nil
}

// loginViaJWT attempts to login via JWT token in the user's config directory.
// Returns an error on failures. This error should be considered nonfatal and the user logged in via
// an alternative method instead.
//
// If a username was given, it will first be matched against the username found in the file.
// NOTE(rlandau): we still perform a whois against the backend later, but this allows us a sanity check without touching the backend.
func loginViaJWT(username string) (err error) {
	var tknbytes []byte
	// NOTE the reversal of standard error checking (`err == nil`)
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
// Returns if the MFA prompt was displayed and fill out (if !mfa, the Client successfully auth'd without MFA)
// Only prints to the log on critical failures
//
// ! Not to be called in script mode.
func promptForInput(prepopUsername string) (mfa bool, err error) {
	// prompt for user name and password
	u, p, err := CredPrompt(prepopUsername)
	if err != nil {
		return false, err
	}

	// log in via u/p
	resp, err := Client.LoginEx(u, p)
	if err != nil {
		return false, err
	} else if resp.LoginStatus {
		// successful login, no need to continue to continue to MFA
		return false, nil
	}

	// not yet logged in, likely due to required MFA
	if resp.MFASetupRequired {
		return false, ErrMFASetupRequired
	} else if !resp.MFARequired {
		// we aren't logged in, but it isn't because MFARequired
		// unknown state, fail out
		clilog.Writer.Criticalf("failed to login, unknown response state: %+v", resp)
		return false, uniques.ErrGeneric
	}

	// prompt for TOTP or recovery code
	code, authType, err := mfaPrompt()
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
	return true, nil

}

// createTokenFile creates a login token for future use.
// The token's path is saved to an environment variable to be looked up on future runs.
//
// Token files have the form:
//
// <username>
//
// <token>
func createTokenFile(username string) error {
	var (
		err   error
		token string
	)
	if token, err = Client.ExportLoginToken(); err != nil {
		return fmt.Errorf("failed to export login token: %v", err)
	}

	// write out the username, then the token
	fd, err := os.OpenFile(cfgdir.DefaultTokenPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create token: %v", err)
	}

	if _, err := fd.WriteString(username + "\n"); err != nil {
		return fmt.Errorf("failed to write token: %v", err)
	}
	if _, err := fd.WriteString(token); err != nil {
		return fmt.Errorf("failed to write token: %v", err)
	}

	if err = fd.Close(); err != nil {
		return fmt.Errorf("failed to close token file: %v", err)
	}

	clilog.Writer.Infof("Created token file @ %v", cfgdir.DefaultTokenPath)
	return nil
}

// End closes the connection to the server and destroys the data in the connection singleton.
// Does not logout the user as to not invalidate existing JWTs.
//
// To reconnect, you will need to call Initialize() again.
func End() error {
	MyInfo = types.UserDetails{}
	if Client == nil { // job's done
		return nil
	}

	if err := Client.Close(); err != nil {
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
	id int32, invalid string, err error,
) {
	id = -1
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
	id, err = Client.CreateScheduledSearch(name, desc, freq,
		uuid.UUID{}, qry, dur, []int32{MyInfo.DefaultGID})
	if err != nil {
		return -1, "", fmt.Errorf("failed to schedule search: %v", err)
	}
	return id, "", nil
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
