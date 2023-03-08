/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/gravwell/gravwell/v3/client/types"
)

var (
	ErrInvalidLogLevel = errors.New("Invalid logging level")
	Version            = VersionStruct{
		Major:    0,
		Minor:    1,
		Revision: 1,
	}
)

type VersionStruct struct {
	Major    uint16
	Minor    uint16
	Revision uint16
}

// String returns the version in the format <Major>.<Minor>.<Revision>, e.g. "4.1.0".
func (v VersionStruct) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Revision)
}

func (c *Client) GetGuiSettings() (types.GUISettings, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.getGuiSettings()
}

func (c *Client) getGuiSettings() (types.GUISettings, error) {
	settings := types.GUISettings{}
	err := c.getStaticURL(SETTINGS_URL, &settings)
	return settings, err

}

// MySessions returns an array of the current user's sessions.
func (c *Client) MySessions() ([]types.Session, error) {
	if c.userDetails.UID == 0 {
		return nil, ErrNotSynced
	}
	return c.Sessions(c.userDetails.UID)
}

// MyInfo returns the current user's information.
func (c *Client) MyInfo() (types.UserDetails, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.getMyInfo()
}

// MyUID returns the current user's numeric user ID.
func (c *Client) MyUID() int32 {
	return c.userDetails.UID
}

// MyAdminStatus returns true if the current user is marked as an administrator.
func (c *Client) MyAdminStatus() bool {
	return c.userDetails.Admin
}

// Groups returns the current user's group memberships.
func (c *Client) Groups() (gps []types.GroupDetails, err error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.userDetails.UID == 0 {
		if err = c.syncNoLock(); err != nil {
			return
		}
	}
	gps = c.userDetails.Groups
	return
}

func (c *Client) getMyInfo() (types.UserDetails, error) {
	dets := types.UserDetails{}
	if err := c.getStaticURL(USER_INFO_URL, &dets); err != nil {
		return dets, err
	}
	return dets, nil
}

// CheckApiVersion assert the REST API version of the webserver is compatible
// with the client.
func (c *Client) CheckApiVersion() (string, error) {
	var version types.VersionInfo
	if err := c.getStaticURL(API_VERSION_URL, &version); err != nil {
		return "", err
	}
	if err := types.CheckApiVersion(version.API); err != nil {
		return err.Error(), nil
	}
	return "", nil
}

// GetApiVersion returns the REST API version of the webserver.
func (c *Client) GetApiVersion() (types.ApiInfo, error) {
	var version types.VersionInfo
	err := c.getStaticURL(API_VERSION_URL, &version)
	return version.API, err
}

func (c *Client) getLogLevelInfo() (types.LoggingLevels, error) {
	ll := types.LoggingLevels{}
	if err := c.methodStaticURL(http.MethodGet, LOGGING_PATH_URL, &ll); err != nil {
		return ll, err
	}
	return ll, nil
}

// GetLogLevel is an admin-only function which returns the webserver's enabled log level.
//
// Valid levels: "Off", "Error", "Warn", "Info", "Web Access".
func (c *Client) GetLogLevel() (string, error) {
	ll, err := c.getLogLevelInfo()
	if err != nil {
		return "", err
	}
	return ll.Current, nil
}

// SetLogLevel is an admin-only function which sets the webserver's logging level.
//
// Valid levels: "Off", "Error", "Warn", "Info", "Web Access".
func (c *Client) SetLogLevel(level string) error {
	//get what is supported
	ll, err := c.getLogLevelInfo()
	if err != nil {
		return err
	}
	ok := false
	//check that what is requested is valid
	for i := range ll.Levels {
		if strings.ToLower(level) == strings.ToLower(ll.Levels[i]) {
			level = ll.Levels[i]
			ok = true
			break
		}
	}
	if !ok {
		return ErrInvalidLogLevel
	}
	l := types.LogLevel{
		Level: level,
	}
	return c.methodStaticPushURL(http.MethodPut, LOGGING_PATH_URL, l, nil)
}

// GetTags returns an array of strings representing the tags on the Gravwell system.
func (c *Client) GetTags() ([]string, error) {
	var tags []string
	err := c.methodStaticURL(http.MethodGet, TAGS_URL, &tags)
	return tags, err
}

// GetLicenseDistributionState checks the distribution status of a newly-uploaded license
// during the initial setup of a Gravwell cluster. This function MUST be called after
// calling InitLicense; when the status returned is "done", Gravwell is ready for use.
func (c *Client) GetLicenseDistributionState() (ds types.LicenseDistributionStatus, err error) {
	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, LICENSE_INIT_STATUS)
	var req *http.Request
	var resp *http.Response
	if req, err = http.NewRequest(http.MethodGet, uri, nil); err != nil {
		return
	}
	if resp, err = c.clnt.Do(req); err != nil {
		c.objLog.Log("WEB "+req.Method+" Error "+err.Error(), req.URL.String(), nil)
		return
	}
	if resp == nil {
		err = errors.New("Invalid response")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("Invalid response %s", resp.Status)
		return
	}
	if err = json.NewDecoder(resp.Body).Decode(&ds); err != nil {
		return
	}
	return
}

// LicenseInitRequired returns true if the Gravwell cluster requires a license.
// If true, use InitLicense to upload a valid license file.
func (c *Client) LicenseInitRequired() bool {
	if _, err := c.GetLicenseDistributionState(); err != nil {
		return false
	}
	return true
}

// InitLicense uploads the contents of a Gravwell license. It will return nil
// if the license is valid and accepted by Gravwell. After calling InitLicense,
// you MUST use GetLicenseDistributionState to verify that Gravwell has distributed
// the license to the indexers and is ready to use.
func (c *Client) InitLicense(b []byte) error {
	bb := bytes.NewBuffer(nil)
	wtr := multipart.NewWriter(bb)
	mp, err := wtr.CreateFormFile(`file`, `license`)
	if err != nil {
		return err
	}
	if n, err := mp.Write(b); err != nil {
		return err
	} else if n != len(b) {
		return errors.New("Failed to create license upload package")
	}
	if err := wtr.Close(); err != nil {
		return err
	}
	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, LICENSE_INIT_UPLOAD)
	req, err := http.NewRequest(http.MethodPost, uri, bb)
	if err != nil {
		return err
	}
	req.Header.Set(`Content-Type`, wtr.FormDataContentType())
	resp, err := c.clnt.Do(req)
	if err != nil {
		c.objLog.Log("WEB "+req.Method+" Error "+err.Error(), req.URL.String(), nil)
		return err
	}
	if resp == nil {
		return errors.New("Invalid response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Invalid response %s", resp.Status)
	}
	return nil
}

// SendMail sends an email with the specified parameters using the mail server configuration
// defined for the current user. Note that the email will be sent from the webserver, not the
// system running the client code.
func (c *Client) SendMail(from string, to []string, subject string, body string, attch []types.UserMailAttachment) error {
	msg := types.UserMail{
		From:        from,
		To:          to,
		Subject:     subject,
		Body:        body,
		Attachments: attch,
	}
	return c.postStaticURL(MAIL_URL, &msg, nil)
}

// SendPrebuiltMail operates as SendMail, but takes a pre-populated types.UserMail object
// as an argument instead of discrete arguments.
func (c *Client) SendPrebuiltMail(msg types.UserMail) error {
	return c.postStaticURL(MAIL_URL, &msg, nil)
}

// ConfigureMail sets up mail server options for the current user.
// The user, pass, server, and port parameters specify the mail server and authentication
// options for the server. The useTLS flag enables TLS for SMTP, and the noVerify flag disables
// checking of TLS certs.
func (c *Client) ConfigureMail(user, pass, server string, port uint16, useTLS, noVerify bool) error {
	msg := types.UserMailConfig{
		Server:             server,
		Username:           user,
		Password:           pass,
		Port:               int(port),
		UseTLS:             useTLS,
		InsecureSkipVerify: noVerify,
	}
	return c.putStaticURL(MAIL_CONFIGURE_URL, &msg, nil)
}

// MailConfig retrieves the current mail config
// if no mail config is set an empty UserMailConfig is returned
// Even on a valid mail config the Password portion is not present in the response
func (c *Client) MailConfig() (mc types.UserMailConfig, err error) {
	err = c.getStaticURL(MAIL_CONFIGURE_URL, &mc)
	return
}

// DeleteMailConfig removes a users mail configuration fom preferences
// this completely uninstalls any mail configs
func (c *Client) DeleteMailConfig() error {
	return c.methodStaticPushURL(http.MethodDelete, MAIL_CONFIGURE_URL, nil, nil, http.StatusOK, http.StatusNotFound)
}

// WellData returns information about the storage wells on the indexers.
// The return value is a map of indexer name strings to IndexerWellData objects.
func (c *Client) WellData() (mp map[string]types.IndexerWellData, err error) {
	err = c.getStaticURL(wellDataUrl(), &mp)
	return
}

// GetLibFile fetches the contents of a particular SOAR library file, as used in
// scheduled search scripts. The repo and commit arguments are optional.
// Examples:
//
//	c.GetLibFile("https://github.com/gravwell/libs", "cd9d6c5", "alerts/email.ank")
//	c.GetLibFile("", "", "utils/links.ank")
func (c *Client) GetLibFile(repo, commit, fn string) (bts []byte, err error) {
	if fn == `` {
		err = errors.New("Missing filename")
		return
	}
	if _, err = url.Parse(repo); err != nil {
		return
	}
	mp := make(map[string]string, 3)
	if repo != `` {
		mp[`repo`] = repo
	}
	if commit != `` {
		mp[`commit`] = commit
	}
	mp[`path`] = fn
	var resp *http.Response
	if resp, err = c.methodParamRequestURL(http.MethodGet, LIBS_URL, mp, nil); err == nil {
		if resp.StatusCode != 200 {
			if err = decodeBodyError(resp.Body); err == nil {
				err = fmt.Errorf("Invalid response code: %s(%d)", resp.Status, resp.StatusCode)
			}
		} else {
			bb := bytes.NewBuffer(nil)
			io.Copy(bb, resp.Body)
			bts = bb.Bytes()
		}
		resp.Body.Close()
	}
	return
}
