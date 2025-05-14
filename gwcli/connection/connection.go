/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Singleton instantiation of the gravwell client library. All calls to the Gravwell instances should
be called via this singleton.

This package also contains some wrapper functions for grav.Client calls where we want to ensure
consistent access and parameters.
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

var Client *grav.Client

var MyInfo types.UserDetails

// Initializes Client using the given connection string of the form <host>:<port>.
// Destroys a pre-existing connection (but does not log out), if there was one.
// restLogPath should be left empty outside of test packages
func Initialize(conn string, UseHttps, InsecureNoEnforceCerts bool, restLogPath string) (err error) {
	if Client != nil {
		Client.Close()
		// TODO should probably close the logger, if possible externally
		Client = nil
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

// struct for passing credentials into Login
type Credentials struct {
	Username     string
	Password     string
	PassfilePath string
}

// Login the initialized Client. Attempts to use a JWT token first, then falls back to supplied
// credentials.
//
// Ineffectual if Client is already logged in.
func Login(cred Credentials, scriptMode bool) (err error) {
	if Client.LoggedIn() {
		return nil
	}

	// login is attempted via JWT token first
	// If any stage in the process fails
	// the error is logged and we fall back to flags and prompting
	if err := LoginViaToken(); err != nil {
		// jwt token failure; log and move on
		clilog.Writer.Warnf("Failed to login via JWT token: %v", err)

		if err = loginViaCredentials(cred, scriptMode); err != nil {
			clilog.Writer.Errorf("Failed to login via credentials: %v", err)
			return err
		}
		clilog.Writer.Infof("Logged in via credentials")

		if err := CreateToken(); err != nil {
			clilog.Writer.Warnf("%v", err.Error())
			// failing to create the token is not fatal
		} /*else {
			// spin up a goroutine to refresh the login token automatically
			go func() {
				// endlessly sleep, refresh token, then sleep again
				for {
					time.Sleep(refreshInterval)
					if err := Client.RefreshLoginToken(); err != nil {
						clilog.Writer.Warnf("failed to refresh JWT: %v", err)
					} else {
						// re-export the new token
						if err := CreateToken(); err != nil {
							clilog.Writer.Warnf("failed to re-create JWT on refresh: %v",
								err.Error())
						}
					}
				}
			}()
		} */
	} else {
		clilog.Writer.Infof("Logged in via JWT")
	}

	// on successfuly login, fetch and cache MyInfo
	if MyInfo, err = Client.MyInfo(); err != nil {
		return errors.New("failed to cache user info: " + err.Error())
	}

	return nil
}

// Attempts to login via JWT token in the user's config directory.
// Returns an error on failures. This error should be considered nonfatal and the user logged in via
// an alternative method instead.
func LoginViaToken() (err error) {
	var tknbytes []byte
	// NOTE the reversal of standard error checking (`err == nil`)
	if tknbytes, err = os.ReadFile(cfgdir.DefaultTokenPath); err == nil {
		if err = Client.ImportLoginToken(string(tknbytes)); err == nil {
			if err = Client.TestLogin(); err == nil {
				return nil
			}
		}
	}
	return
}

// Attempts to login via the given credentials struct.
// A given password takes precedence over a passfile.
func loginViaCredentials(cred Credentials, scriptMode bool) error {
	// check for password in file
	if strings.TrimSpace(cred.Password) == "" {
		if cred.PassfilePath != "" {
			b, err := os.ReadFile(cred.PassfilePath)
			if err != nil {
				return fmt.Errorf("failed to read password from %v: %v", cred.PassfilePath, err)
			}
			cred.Password = strings.TrimSpace(string(b))
		}
	}

	if cred.Username == "" || cred.Password == "" {
		// if script mode, do not prompt
		if scriptMode {
			return fmt.Errorf("no valid token found.\n" +
				"Please login via --username and {--password | --passfile}")
		}

		// prompt for credentials
		credM, err := CredPrompt(cred.Username, cred.Password)
		if err != nil {
			return err
		}
		// pull input results
		if finalCredM, ok := credM.(credModel); !ok {
			return err
		} else if finalCredM.killed {
			return errors.New("you must authenticate to use gwcli")
		} else {
			cred.Username = finalCredM.UserTI.Value()
			cred.Password = finalCredM.PassTI.Value()
		}
	}

	return Client.Login(cred.Username, cred.Password)
}

// Creates a login token for future use.
// The token's path is saved to an environment variable to be looked up on future runs
func CreateToken() error {
	var (
		err   error
		token string
	)
	if token, err = Client.ExportLoginToken(); err != nil {
		return fmt.Errorf("failed to export login token: %v", err)
	}

	// write out the token
	fd, err := os.OpenFile(cfgdir.DefaultTokenPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create token: %v", err)
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

// Closes the connection to the server.
// Does not logout the user as to not invalidate existing JWTs.
func End() error {
	if Client == nil {
		return nil
	}

	Client.Close()
	return nil
}

// A validation wrapper around Client.CreateScheduledSearch to provide consistent
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

// Validates and submits the given query to the connected server instance.
// Duration must be negative or zero (X time units back in time from now()).
// A positive duration will result in an error.
//
// Returns a handle to executing searching.
func StartQuery(qry string, durFromNow time.Duration) (grav.Search, error) {
	var err error
	if durFromNow > 0 {
		return grav.Search{}, fmt.Errorf("duration must be negative or zero (given %v)", durFromNow)
	}

	// validate search query
	if err = Client.ParseSearch(qry); err != nil {
		return grav.Search{}, fmt.Errorf("'%s' is not a valid query: %s", qry, err.Error())
	}

	// check for scheduling

	end := time.Now()
	sreq := types.StartSearchRequest{
		SearchStart:  end.Add(durFromNow).Format(uniques.SearchTimeFormat),
		SearchEnd:    end.Format(uniques.SearchTimeFormat),
		Background:   false,
		SearchString: qry, // pull query from the commandline
		NoHistory:    false,
		Preview:      false,
	}
	clilog.Writer.Infof("Executing foreground search '%v' from %v -> %v",
		sreq.SearchString, sreq.SearchStart, sreq.SearchEnd)
	s, err := Client.StartSearchEx(sreq)
	return s, err

}

// Maps Render module and csv/json flag state to a string usable with DownloadSearch().
// JSON, then CSV, take precidence over a direct render -> format map.
// If a better renderer type cannot be determined, Archive will be selected.
func renderToDownload(rndr string, csv, json bool) string {
	if json {
		return types.DownloadJSON
	}
	if csv {
		return types.DownloadCSV
	}
	switch rndr {
	case types.RenderNameHex, types.RenderNameRaw, types.RenderNameText:
		return types.DownloadText
	case types.RenderNamePcap:
		return types.DownloadPCAP
	default:
		return types.DownloadArchive
	}
}

// Downloads the given search according to its renderer (or CSV/JSON, if given).
func DownloadSearch(search *grav.Search, tr types.TimeRange, csv, json bool) (
	rc io.ReadCloser, format string, err error,
) {
	format = renderToDownload(search.RenderMod, csv, json)
	clilog.Writer.Infof("renderer '%s' -> '%s'", search.RenderMod, format)
	rc, err = Client.DownloadSearch(search.ID, tr, format)
	return
}

// Returns a consistent sting for a successful query result download
func DownloadQuerySuccessfulString(filename string, append bool, format string) string {
	var word string = "wrote"
	if append {
		word = "appended"
	}
	return fmt.Sprintf("Successfully %v %v results to %v", word, format, filename)
}
