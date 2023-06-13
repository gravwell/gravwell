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
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/client/types"

	"github.com/google/uuid"
)

const (
	maxLicenseSize int64 = 1024 * 8 //8k is almost 6X what is normal

	backupFormFile string = `backup`
)

var (
	ErrNotAdmin = errors.New("You are not an admin")
)

// IsAdmin checks if the logged-in user is an admin.
func (c *Client) IsAdmin() (bool, error) {
	dts, err := c.MyInfo()
	if err != nil {
		return false, err
	}
	return dts.Admin, nil
}

// LockUserAccount (admin-only) locks a user account. The user will be unable
// to log in until unlocked, and all existing sessions will be terminated.
func (c *Client) LockUserAccount(id int32) error {
	return c.putStaticURL(lockUrl(id), nil)
}

// LockUserAccount (admin-only) unlocks a user account.
func (c *Client) UnlockUserAccount(id int32) error {
	return c.deleteStaticURL(lockUrl(id), nil)
}

// AddUser (admin-only) creates a new user. The user and pass parameters specify login information.
// The name parameter is the user's real name and the email parameter is the user's
// email address. If 'admin' is set to true, the user will be flagged as an administrator.
func (c *Client) AddUser(user, pass, name, email string, admin bool) error {
	userDetails := types.AddUser{
		User:  user,
		Pass:  pass,
		Name:  name,
		Email: email,
		Admin: admin,
	}
	if err := c.postStaticURL(ADD_USER_URL, userDetails, nil); err != nil {
		return err
	}
	return nil
}

// DeleteUser (admin-only) deletes the specified user.
func (c *Client) DeleteUser(id int32) error {
	if err := c.deleteStaticURL(usersInfoUrl(id), nil); err != nil {
		return err
	}
	return nil
}

// GetUserInfo (admin-only) gets information about a specific user.
func (c *Client) GetUserInfo(id int32) (types.UserDetails, error) {
	udet := types.UserDetails{}
	if err := c.methodStaticURL(http.MethodGet, usersInfoUrl(id), &udet); err != nil {
		return udet, err
	}
	return udet, nil
}

// SetAdmin (admin-only) changes the admin status for the user with the given ID.
func (c *Client) SetAdmin(id int32, admin bool) error {
	var method string
	resp := types.AdminActionResp{}
	if admin {
		method = http.MethodPut
	} else {
		method = http.MethodDelete
	}
	if err := c.methodStaticURL(method, usersAdminUrl(id), &resp); err != nil {
		return err
	}
	if resp.UID != id || resp.Admin != admin {
		return errors.New("Server responded with state other than requested")
	}
	return nil
}

// changePass will change a users password
func (c *Client) changePass(id int32, req types.ChangePassword) error {
	if err := c.putStaticURL(usersChangePassUrl(id), req); err != nil {
		return err
	}
	return nil
}

// AdminChangePass (admin-only) changes the specified user's password without
// requiring the current password.
func (c *Client) AdminChangePass(id int32, pass string) error {
	req := types.ChangePassword{
		NewPass: pass,
	}
	return c.changePass(id, req)
}

// UserChangePass changes the given user's password. Any user may change
// their own password, but they must know the current password.
func (c *Client) UserChangePass(id int32, orig, pass string) error {
	req := types.ChangePassword{
		OrigPass: orig,
		NewPass:  pass,
	}
	return c.changePass(id, req)
}

// SetDefaultSearchGroup will set the specified user's default search group.
// Admins can set any user's default search group, but regular users can only set their own.
func (c *Client) SetDefaultSearchGroup(uid int32, gid int32) error {
	req := types.UserDefaultSearchGroup{
		GID: gid,
	}
	return c.methodStaticPushURL(http.MethodPut, usersSearchGroupUrl(uid), req, nil)
}

// GetDefaultSearchGroup returns the specified users default search group
// Admins can get any user's default search group, but regular users can only get their own.
func (c *Client) GetDefaultSearchGroup(uid int32) (gid int32, err error) {
	err = c.getStaticURL(usersSearchGroupUrl(uid), &gid)
	return
}

// DeleteDefaultSearchGroup removes the default search group for a specified user
// Admins can delete any user's default search group, but regular users can only delete their own.
func (c *Client) DeleteDefaultSearchGroup(uid int32) error {
	return c.deleteStaticURL(usersSearchGroupUrl(uid), nil)
}

// AdminUpdateInfo changes basic information about the specified user.
// Admins can set any user's info, but regular users can only set their own.
func (c *Client) UpdateUserInfo(id int32, user, name, email string) error {
	req := types.UpdateUser{
		User:  user,
		Name:  name,
		Email: email,
	}
	return c.methodStaticPushURL(http.MethodPut, usersInfoUrl(id), req, nil)
}

// AddGroup (admin-only) creates a new group with the given name and description.
func (c *Client) AddGroup(name, desc string) error {
	gpInfo := types.AddGroup{
		Name: name,
		Desc: desc,
	}
	return c.postStaticURL(groupUrl(), gpInfo, nil)
}

// DeleteGroup (admin-only) will delete a group.
func (c *Client) DeleteGroup(gid int32) error {
	return c.deleteStaticURL(groupIdUrl(gid), nil)
}

// UpdateGroup (admin-only) will update the specified group's details.
func (c *Client) UpdateGroup(gid int32, gdet types.GroupDetails) error {
	return c.putStaticURL(groupIdUrl(gid), gdet)
}

// GetAllUsers returns information about all users on the system.
func (c *Client) GetAllUsers() ([]types.UserDetails, error) {
	var users []types.UserDetails
	if err := c.getStaticURL(allUsersUrl(), &users); err != nil {
		return nil, err
	}
	return users, nil
}

// AddUserToGroup adds a user to a group.
func (c *Client) AddUserToGroup(uid, gid int32) error {
	uag := types.UserAddGroups{
		GIDs: []int32{gid},
	}
	return c.postStaticURL(usersGroupUrl(uid), uag, nil)
}

// DeleteUserFromGroup removes a user from a group.
func (c *Client) DeleteUserFromGroup(uid, gid int32) error {
	return c.deleteStaticURL(usersGroupIdUrl(uid, gid), nil)
}

// ListGroups returns information about groups to which the user belongs.
func (c *Client) GetUserGroups(uid int32) ([]types.GroupDetails, error) {
	var udet types.UserDetails
	if err := c.getStaticURL(usersInfoUrl(uid), &udet); err != nil {
		return nil, err
	}
	return udet.Groups, nil
}

// GetGroups returns information about all groups on the system.
func (c *Client) GetGroups() ([]types.GroupDetails, error) {
	var gps []types.GroupDetails
	if err := c.getStaticURL(groupUrl(), &gps); err != nil {
		return nil, err
	}
	return gps, nil
}

// GetGroupMap returns a map of GID to group name for every group on the system.
func (c *Client) GetGroupMap() (map[int32]string, error) {
	var gps []types.GroupDetails
	if err := c.getStaticURL(groupUrl(), &gps); err != nil {
		return nil, err
	}
	m := make(map[int32]string, len(gps))
	for _, g := range gps {
		m[g.GID] = g.Name
	}
	return m, nil
}

// GetUserMap returns a map of UID to username for every user on the system.
func (c *Client) GetUserMap() (map[int32]string, error) {
	var uds []types.UserDetails
	if err := c.getStaticURL(allUsersUrl(), &uds); err != nil {
		return nil, err
	}
	m := make(map[int32]string, len(uds))
	for _, u := range uds {
		m[u.UID] = u.User
	}
	return m, nil
}

// GetGroup returns information about the specified group.
func (c *Client) GetGroup(id int32) (types.GroupDetails, error) {
	var gp types.GroupDetails
	if err := c.getStaticURL(groupIdUrl(id), &gp); err != nil {
		return gp, err
	}
	return gp, nil
}

// ListGroupUsers will return user details for all members of a group.
// Only administrators or members of the group may call this function.
func (c *Client) GetGroupUsers(gid int32) ([]types.UserDetails, error) {
	var udets []types.UserDetails
	if err := c.getStaticURL(groupMembersUrl(gid), &udets); err != nil {
		return nil, err
	}
	return udets, nil
}

// GetAllDashboards (admin-only) returns a list of all dashboards on the system.
func (c *Client) GetAllDashboards() ([]types.Dashboard, error) {
	var dbs []types.Dashboard
	if err := c.getStaticURL(allDashboardUrl(), &dbs); err != nil {
		return nil, err
	}
	return dbs, nil
}

// GetUserGroupsDashboards returns a list of all dashboards the current user can view.
func (c *Client) GetUserGroupsDashboards() ([]types.Dashboard, error) {
	var dbs []types.Dashboard
	if err := c.getStaticURL(myDashboardUrl(), &dbs); err != nil {
		return nil, err
	}
	return dbs, nil
}

// GetUserDashboards returns a list of all dashboards belonging to the specified user.
// Only admins or the user in question may call this function.
func (c *Client) GetUserDashboards(id int32) ([]types.Dashboard, error) {
	var dbs []types.Dashboard
	if err := c.getStaticURL(userDashboardUrl(id), &dbs); err != nil {
		return nil, err
	}
	return dbs, nil
}

// GetGroupDashboards returns a list of all dashboards shared with the specified group.
// Only admins or members of the group may call this function.
func (c *Client) GetGroupDashboards(id int32) ([]types.Dashboard, error) {
	var dbs []types.Dashboard
	if err := c.getStaticURL(groupDashboardUrl(id), &dbs); err != nil {
		return nil, err
	}
	return dbs, nil
}

// GetDashboard fetches a dashboard by numeric ID.
func (c *Client) GetDashboard(id uint64) (types.Dashboard, error) {
	var db types.Dashboard
	if err := c.getStaticURL(dashboardUrl(id), &db); err != nil {
		return db, err
	}
	return db, nil
}

// GetDashboardByGuid fetches a dashboard by GUID.
func (c *Client) GetDashboardByGuid(guid string) (types.Dashboard, error) {
	var db types.Dashboard
	if err := c.getStaticURL(dashboardUrlString(guid), &db); err != nil {
		return db, err
	}
	return db, nil
}

// DeleteDashboard deletes the specified dashboard.
func (c *Client) DeleteDashboard(id uint64) error {
	return c.deleteStaticURL(dashboardUrl(id), nil)
}

// DeleteDashboardByGuid deletes a dashboard specified by GUID.
func (c *Client) DeleteDashboardByGuid(id string) error {
	return c.deleteStaticURL(dashboardUrlString(id), nil)
}

// CloneDashboard creates a copy of a dashboard and returns the ID of the new dashboard.
func (c *Client) CloneDashboard(origid uint64) (id uint64, err error) {
	err = c.getStaticURL(cloneDashboardUrl(origid), &id)
	return
}

// AddDashboard creates a new dashboard and returns the ID. The obj parameter will be
// stored as the Data field of the dashboard.
func (c *Client) AddDashboard(name, desc string, obj interface{}) (uint64, error) {
	dbAdd, err := types.EncodeDashboardAdd(name, desc, obj)
	if err != nil {
		return 0, err
	}
	var id uint64
	err = c.postStaticURL(myDashboardUrl(), dbAdd, &id)
	return id, err
}

// UpdateDashboard takes a types.Dashboard as an argument and updates the corresponding
// dashboard on the server to match.
func (c *Client) UpdateDashboard(db *types.Dashboard) error {
	return c.putStaticURL(dashboardUrl(db.ID), db)
}

// Sessions lists sessions for the specified user.
func (c *Client) Sessions(id int32) ([]types.Session, error) {
	userSessResp := types.UserSessions{}

	if err := c.getStaticURL(sessionsUrl(id), &userSessResp); err != nil {
		return nil, err
	}
	return userSessResp.Sessions, nil

}

// GetPreferences fetches the preferences structure for the user and unpacks them into obj.
func (c *Client) GetPreferences(id int32, obj interface{}) error {
	return c.getStaticURL(preferencesUrl(id), obj)
}

// DeletePreferences clear's the specified user's preferences.
func (c *Client) DeletePreferences(id int32) error {
	return c.deleteStaticURL(preferencesUrl(id), nil)
}

// GetMyPreferences gets the current user's preferences into obj.
func (c *Client) GetMyPreferences(obj interface{}) error {
	if c.userDetails.UID == 0 {
		return ErrNotSynced
	}
	return c.GetPreferences(c.userDetails.UID, obj)
}

// PutPreferences updates the specified user's preferences with obj.
func (c *Client) PutPreferences(id int32, obj interface{}) error {
	return c.putStaticURL(preferencesUrl(id), obj)
}

// PutMyPreferences updates the current user's preferences with obj.
func (c *Client) PutMyPreferences(obj interface{}) error {
	if c.userDetails.UID == 0 {
		return ErrNotSynced
	}
	return c.putStaticURL(preferencesUrl(c.userDetails.UID), obj)
}

// GetAllPreferences (admin-only) fetches preferences for all users.
func (c *Client) GetAllPreferences() (types.UserPreferences, error) {
	var prefs types.UserPreferences
	if err := c.getStaticURL(allPreferencesUrl(), &prefs); err != nil {
		return nil, err
	}
	return prefs, nil
}

// GetLicenseInfo returns information about the currently installed license.
func (c *Client) GetLicenseInfo() (li types.LicenseInfo, err error) {
	err = c.getStaticURL(licenseInfoUrl(), &li)
	return
}

// GetLicenseSKU returns the SKU for the license in use.
func (c *Client) GetLicenseSKU() (sku string, err error) {
	err = c.getStaticURL(licenseSKUUrl(), &sku)
	return
}

// GetLicenseSerial returns the serial number for the current license.
func (c *Client) GetLicenseSerial() (serial string, err error) {
	err = c.getStaticURL(licenseSerialUrl(), &serial)
	return
}

// UploadLicenseFile is an admin-only function to upload a new license to the Gravwell
// system. It takes a path to a license file as the argument.
func (c *Client) UploadLicenseFile(f string) ([]types.LicenseUpdateError, error) {
	fin, err := os.Open(f)
	if err != nil {
		return nil, err
	}
	defer fin.Close()
	fi, err := fin.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() <= 0 {
		return nil, errors.New("License file is empty")
	}
	if fi.Size() > maxLicenseSize {
		return nil, errors.New("License file is too large")
	}

	bb := bytes.NewBuffer(nil)
	wtr := multipart.NewWriter(bb)
	mp, err := wtr.CreateFormFile(`file`, `license`)
	if err != nil {
		return nil, err
	}

	if _, err := io.Copy(mp, fin); err != nil {
		return nil, err
	}
	if err := wtr.Close(); err != nil {
		return nil, err
	}
	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, LICENSE_UPDATE_URL)
	req, err := http.NewRequest(http.MethodPost, uri, bb)
	if err != nil {
		return nil, err
	}
	req.Header.Set(`Content-Type`, wtr.FormDataContentType())
	var warnings []types.LicenseUpdateError
	okResps := []int{http.StatusOK, http.StatusMultiStatus}
	if err := c.staticRequest(req, &warnings, okResps); err != nil {
		if err != io.EOF {
			return nil, err
		}
		//if the error is EOF, it means that there was no response
		//which means total success!
	}
	return warnings, nil
}

// Impersonate is an admin-only function which can be used to execute commands
// as another user, similar to the `su` command on Unix. It returns a Client
// object which is authenticated as the specified user.
func (c *Client) Impersonate(uid int32) (nc *Client, err error) {
	var loginResp types.LoginResponse
	if err = c.methodStaticURL(http.MethodGet, usersAdminImpersonate(uid), &loginResp); err != nil {
		return
	}
	//create the header map and stuff our user-agent in there
	hdrMap := newHeaderMap()
	hdrMap.add(`User-Agent`, c.userAgent)
	hdrMap.add(authHeaderName, "Bearer "+loginResp.JWT)

	sessData := ActiveSession{
		JWT: loginResp.JWT,
	}
	//generate a new client
	nc = &Client{
		server:      c.server,
		serverURL:   c.serverURL,
		hm:          hdrMap,
		qm:          newQueryMap(),
		clnt:        c.clnt,
		timeout:     defaultRequestTimeout,
		mtx:         &sync.Mutex{},
		state:       STATE_AUTHED,
		enforceCert: c.enforceCert,
		sessionData: sessData,
		httpScheme:  c.httpScheme,
		wsScheme:    c.wsScheme,
		objLog:      c.objLog,
		transport:   c.transport,
		userAgent:   c.userAgent,
	}
	var dets types.UserDetails
	if dets, err = nc.getMyInfo(); err != nil {
		return
	}
	if dets.UID != uid {
		nc = nil
		err = fmt.Errorf("Failed to impersonate new user: %s[%d] != %d", dets.Name, dets.UID, uid)
		return
	}
	//set the user details the client is ready to use right out of the gate
	nc.userDetails = dets
	return
}

// AddIndexer (admin-only) tells the webserver to connect to a new indexer.
// The indexer will be added to the list of indexers in the webserver's config
// file and persist in the future.
func (c *Client) AddIndexer(dialstring string) (map[string]string, error) {
	req := types.IndexerRequest{DialString: dialstring}

	var errors map[string]string
	err := c.postStaticURL(addIndexerUrl(), req, &errors)
	return errors, err
}

// ExtractionSupportedEngines returns a list of valid engines for use in
// autoextraction definitions.
func (c *Client) ExtractionSupportedEngines() (v []string, err error) {
	err = c.getStaticURL(extractionEnginesUrl(), &v)
	return
}

// GetExtractions returns the list of autoextraction definitions available
// to the current user.
func (c *Client) GetExtractions() (dfs []types.AXDefinition, err error) {
	err = c.getStaticURL(extractionsUrl(), &dfs)
	return
}

// GetExtraction returns a particular extraction by UUID
func (c *Client) GetExtraction(uuid string) (d types.AXDefinition, err error) {
	err = c.getStaticURL(extractionIdUrl(uuid), &d)
	return
}

// DeleteExtraction deletes the specified autoextraction.
func (c *Client) DeleteExtraction(uuid string) (wrs []types.WarnResp, err error) {
	if err = c.deleteStaticURL(extractionIdUrl(uuid), nil); err == io.EOF {
		err = nil
	}
	return
}

// TestAddExtraction validates an autoextractor definition.
func (c *Client) TestAddExtraction(d types.AXDefinition) (wrs []types.WarnResp, err error) {
	if err = d.Validate(); err != nil {
		return
	}
	if err = c.postStaticURL(extractionsTestUrl(), d, nil); err == io.EOF {
		err = nil
	}
	return
}

// AddExtraction installs an autoextractor definition, returning the UUID of the new
// extraction or an error if it is invalid.
func (c *Client) AddExtraction(d types.AXDefinition) (id uuid.UUID, wrs []types.WarnResp, err error) {
	if err = d.Validate(); err != nil {
		return
	}
	if err = c.postStaticURL(extractionsUrl(), d, &id); err == io.EOF {
		err = nil
	}
	return
}

// UpdateExtraction modifies an existing autoextractor. The UUID field of the definition
// passed in must match the UUID of an existing definition owned by the user.
func (c *Client) UpdateExtraction(d types.AXDefinition) (wrs []types.WarnResp, err error) {
	if err = d.Validate(); err != nil {
		return
	}
	if err = c.methodStaticPushURL(http.MethodPut, extractionsUrl(), d, nil); err == io.EOF {
		err = nil
	}
	return
}

// UploadExtraction uploads a TOML-formatted byteslice containing one or more autoextractor
// definitions. Gravwell will parse these definitions and install or update autoextractors
// as appropriate.
func (c *Client) UploadExtraction(b []byte) (wrs []types.WarnResp, err error) {
	var part io.Writer
	var resp *http.Response
	bb := new(bytes.Buffer)
	wtr := multipart.NewWriter(bb)
	if part, err = wtr.CreateFormFile(`extraction`, `extraction`); err != nil {
		return
	} else if _, err = part.Write(b); err != nil {
		return
	}
	if err = wtr.Close(); err != nil {
		return
	}
	resp, err = c.methodRequestURL(http.MethodPost, extractionsUploadUrl(), wtr.FormDataContentType(), bb)
	if err != nil {
		return
	}
	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("Bad Status %s(%d): %v", resp.Status, resp.StatusCode, getBodyErr(resp.Body))
		return
	}
	err = resp.Body.Close()
	return
}

// Backup generates a complete backup of all content on the Gravwell webserver and writes
// it out to the io.Writer provided. By default, scheduled searches / scheduled scripts are
// not included; set the 'includeSS' option to include them.
func (c *Client) Backup(wtr io.Writer, includeSS bool) (err error) {
	cfg := types.BackupConfig{IncludeSS: includeSS}
	return c.BackupWithConfig(wtr, cfg)
}

func (c *Client) BackupWithConfig(wtr io.Writer, cfg types.BackupConfig) (err error) {
	var resp *http.Response
	dlr := net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: time.Second,
	}
	//backups can take a long time to make, so we have to tweak the client a bit
	tr := http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dlr.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          1,
		IdleConnTimeout:       time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       c.tlsConfig, //grab tls config from client
	}
	clnt := http.Client{
		Transport:     &tr,
		CheckRedirect: redirectPolicy,  //use default redirect policy
		Timeout:       5 * time.Minute, // these requests might take a REALLY long time, so jack the timeout way up
		Jar:           c.clnt.Jar,
	}
	uri := backupUrl()
	var req *http.Request
	if req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, uri), nil); err != nil {
		return
	}
	c.hm.populateRequest(req.Header) // add in the headers

	var vals url.Values
	if vals, err = url.ParseQuery(c.qm.encode()); err != nil {
		return
	}
	// add in any queries like ?admin=true
	if cfg.IncludeSS {
		vals.Add(`savedsearch`, `true`)
	}
	if cfg.OmitSensitive {
		vals.Add(`omit_sensitive`, `true`)
	}
	req.URL.RawQuery = vals.Encode()
	req.Header.Add("Password", cfg.Password)
	if resp, err = clnt.Do(req); err == nil {
		c.objLog.Log(http.MethodGet+" "+resp.Status, uri, nil)
	} else {
		c.objLog.Log(http.MethodGet+" "+err.Error(), uri, nil)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err = fmt.Errorf("Invalid response %s(%d)", resp.Status, resp.StatusCode)
	} else if _, err = io.Copy(wtr, resp.Body); err != nil {
		err = fmt.Errorf("Failed to download complete backup package: %w", err)
	}

	return
}

// Restore reads a backup archive from rdr and unpacks it on the Gravwell server.
func (c *Client) Restore(rdr io.Reader) (err error) {
	var resp *http.Response
	if resp, err = c.uploadMultipartFile(backupUrl(), backupFormFile, `file`, rdr, nil); err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if err = decodeBodyError(resp.Body); err != nil {
			err = fmt.Errorf("response error status %d %v", resp.StatusCode, err)
		} else {
			err = fmt.Errorf("Invalid response %s(%d)", resp.Status, resp.StatusCode)
		}
	}
	return
}

// RestoreEncrypted reads a backup archive from rdr and unpacks it on the Gravwell server.
func (c *Client) RestoreEncrypted(rdr io.Reader, password string) (err error) {
	var resp *http.Response
	if resp, err = c.uploadMultipartFile(backupUrl(), backupFormFile, `file`, rdr, map[string]string{"password": password}); err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if err = decodeBodyError(resp.Body); err != nil {
			err = fmt.Errorf("response error status %d %v", resp.StatusCode, err)
		} else {
			err = fmt.Errorf("Invalid response %s(%d)", resp.Status, resp.StatusCode)
		}
	}
	return
}

// DistributedWebservers queries to determine if the webserver is in distributed mode
// and therefore using the datastore.  This means that certain resource changes may take some
// time to fully distribute. This is an admin-only function.
func (c *Client) DeploymentInfo() (di types.DeploymentInfo, err error) {
	err = c.getStaticURL(deploymentUrl(), &di)

	return
}

// PurgeUser will first enumerate every asset that is owned by the user and delete them
// then it will delete the user. This is an admin-only function.
func (c *Client) PurgeUser(id int32) error {
	//impersonate the user
	nc, err := c.Impersonate(id)
	if err != nil {
		return fmt.Errorf("Failed to impersonate %d - %w", id, err)
	}
	//enumerate and delete user assets

	//persistent searches
	if ss, err := nc.ListSearchStatuses(); err != nil {
		return fmt.Errorf("Failed to list search statuses %w", err)
	} else if len(ss) > 0 {
		for _, s := range ss {
			if s.UID == id {
				if err = nc.DeleteSearch(s.ID); err != nil {
					return fmt.Errorf("Failed to delete user search %v %w", s.ID, err)
				}
			}
		}
	}

	//scheduled searches
	if ss, err := nc.GetScheduledSearchList(); err != nil {
		return fmt.Errorf("Failed to get the users scheduled searches %d %w", id, err)
	} else if len(ss) > 0 {
		for _, s := range ss {
			if s.Owner == id {
				if err := nc.DeleteScheduledSearch(s.ID); err != nil {
					return fmt.Errorf("Failed to purge scheduled searches %d %d %w", id, s.ID, err)
				}
			}
		}
	}

	//user files
	if ufs, err := nc.UserFiles(); err != nil {
		return fmt.Errorf("Failed to get user files %d %w", id, err)
	} else if len(ufs) > 0 {
		for _, uf := range ufs {
			if uf.UID == id {
				if err := nc.DeleteUserFile(uf.GUID); err != nil {
					return fmt.Errorf("Failed to purge user file %v %w", uf.GUID, err)
				}
			}
		}
	}

	//kits
	if ks, err := nc.ListKits(); err != nil {
		return fmt.Errorf("Failed to list kits %w", err)
	} else if len(ks) > 0 {
		for _, k := range ks {
			if k.UID == id {
				if err := nc.ForceDeleteKit(k.ID); err != nil {
					return fmt.Errorf("Failed to purge user kit %v - %w", k.ID, err)
				}
			}
		}
	}

	//kit builds
	if kbs, err := nc.ListKitBuildHistory(); err != nil {
		return fmt.Errorf("Failed to list kit build history %w", err)
	} else if len(kbs) > 0 {
		for _, k := range kbs {
			if err := nc.DeleteKitBuildHistory(k.ID); err != nil {
				return fmt.Errorf("Failed to purge user kit build request %v - %w", k.ID, err)
			}
		}
	}

	//actionables
	if pvs, err := nc.ListPivots(); err != nil {
		return fmt.Errorf("Failed to list pivots %w", err)
	} else if len(pvs) > 0 {
		for _, p := range pvs {
			if p.UID == id {
				if err = nc.DeletePivot(p.GUID); err != nil {
					return fmt.Errorf("Failed to purge user pivots %v - %w", p.GUID, err)
				}
			}
		}
	}

	//macros
	if ms, err := nc.GetUserMacros(id); err != nil {
		return fmt.Errorf("Failed to list macros %w", err)
	} else if len(ms) > 0 {
		for _, p := range ms {
			if p.UID == id {
				if err = nc.DeleteMacro(p.ID); err != nil {
					return fmt.Errorf("Failed to delete user macro %v - %w", p.ID, err)
				}
			}
		}
	}

	//API tokens
	if toks, err := nc.ListTokens(); err != nil {
		return fmt.Errorf("failed to get user API tokens %w", err)
	} else if len(toks) > 0 {
		for _, t := range toks {
			if t.UID == id {
				if err := nc.DeleteToken(t.ID); err != nil {
					return fmt.Errorf("Failed to delete user token %v - %w", t.ID, err)
				}
			}
		}
	}

	//extractors
	if exts, err := nc.GetExtractions(); err != nil {
		return fmt.Errorf("Failed to get user autoextractors %w", err)
	} else if len(exts) > 0 {
		for _, e := range exts {
			if e.UID == id {
				if _, err := nc.DeleteExtraction(e.UUID.String()); err != nil {
					return fmt.Errorf("Failed to delete user extraction %v - %w", e.UUID, err)
				}
			}
		}
	}

	//resources
	if rsr, err := nc.GetResourceList(); err != nil {
		return fmt.Errorf("Failed to get user resource list %w", err)
	} else if len(rsr) > 0 {
		for _, r := range rsr {
			if r.UID == id {
				if err := nc.DeleteResource(r.GUID); err != nil {
					return fmt.Errorf("Failed to delete user resource %v %w", r.GUID, err)
				}
			}
		}
	}

	//templates
	if tmpls, err := nc.ListTemplates(); err != nil {
		return fmt.Errorf("Failed to get user templates %w", err)
	} else if len(tmpls) > 0 {
		for _, t := range tmpls {
			if t.UID == id {
				if err := nc.DeleteTemplate(t.GUID); err != nil {
					return fmt.Errorf("Failed to delete user template %v %w", t.GUID, err)
				}
			}
		}
	}

	//playbooks
	if pbs, err := nc.GetUserPlaybooks(); err != nil {
		return fmt.Errorf("Failed to get user playbooks %d %w", id, err)
	} else if len(pbs) > 0 {
		for _, pb := range pbs {
			if pb.UID == id {
				if err := nc.DeletePlaybook(pb.GUID); err != nil {
					return fmt.Errorf("Failed to purge user playbook %v %w", pb.GUID, err)
				}
			}
		}
	}

	//dashboards
	if dbs, err := nc.GetUserDashboards(id); err != nil {
		return fmt.Errorf("Failed to get user dashboards %d %w", id, err)
	} else if len(dbs) > 0 {
		for _, db := range dbs {
			if db.UID == id {
				if err := nc.DeleteDashboard(db.ID); err != nil {
					return fmt.Errorf("Failed to delete user dashboard %d %w", db.ID, err)
				}
			}
		}
	}

	//query library
	if sls, err := nc.ListSearchLibrary(); err != nil {
		return fmt.Errorf("Failed to get user search library list %w", err)
	} else if len(sls) > 0 {
		for _, sl := range sls {
			if sl.UID == id {
				if err := nc.DeleteSearchLibrary(sl.GUID); err != nil {
					return fmt.Errorf("Failed to delete user search library %v %w", sl.GUID, err)
				}
			}
		}
	}

	//preferences
	if err := nc.DeletePreferences(id); err != nil {
		return fmt.Errorf("Failed to purge user preferences %d %w", id, err)
	}

	if err := nc.Close(); err != nil {
		return fmt.Errorf("Failed to close impersonated client during purge %w", err)
	}

	return c.DeleteUser(id) //finally, delete the user
}

func (c *Client) ForgetIngester(id uuid.UUID) (err error) {
	return c.deleteStaticURL(fmt.Sprintf(INGESTERS_TRACKING_URL, id), nil)
}
