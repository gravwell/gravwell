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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
)

const (
	defaultDownloadCookieDuration time.Duration = 3 * time.Second
	urlSidParamKey                              = `sid`
)

var (
	ErrNotAuthed = errors.New("Not Authed")
	ErrNotFound  = errors.New("Not Found")

	//helper that calls out ok responses as just 200
	stdOk = []int{http.StatusOK}
)

type urlParam struct {
	key   string
	value string
}

func ezParam(name string, val interface{}) urlParam {
	return urlParam{key: name, value: fmt.Sprintf("%v", val)}
}

type ClientError struct {
	Status     string
	StatusCode int
	ErrorBody  string
}

func (e *ClientError) Error() string {
	return fmt.Sprintf("Bad Status %s(%d): %s", e.Status, e.StatusCode, e.ErrorBody)
}

func (c *Client) getStaticURL(url string, obj interface{}, params ...urlParam) error {
	return c.methodStaticURL(http.MethodGet, url, obj, params...)
}

func (c *Client) putStaticURL(url string, obj interface{}, params ...urlParam) error {
	return c.methodStaticPushURL(http.MethodPut, url, obj, nil, nil, params)
}

func (c *Client) putStaticRawURL(url string, data []byte, params ...urlParam) error {
	return c.methodStaticPushRawURL(http.MethodPut, url, data, nil, nil, params)
}
func (c *Client) patchStaticURL(url string, obj interface{}, params ...urlParam) error {
	return c.methodStaticPushURL(http.MethodPatch, url, obj, nil, nil, params)
}

func (c *Client) postStaticURL(url string, sendObj, recvObj interface{}, params ...urlParam) error {
	return c.methodStaticPushURL(http.MethodPost, url, sendObj, recvObj, nil, params)
}

func (c *Client) deleteStaticURL(url string, sendObj interface{}, params ...urlParam) error {
	return c.methodStaticPushURL(http.MethodDelete, url, sendObj, nil, nil, params)
}

func (c *Client) methodStaticURL(method, url string, obj interface{}, params ...urlParam) error {
	if c.state != STATE_AUTHED {
		return ErrNoLogin
	}
	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, url)
	req, err := http.NewRequest(method, uri, nil)
	if err != nil {
		return err
	}
	return c.staticRequest(req, obj, nil, params)
}

func addParams(req *http.Request, params []urlParam) {
	if len(params) > 0 {
		q := req.URL.Query()
		for _, p := range params {
			q.Add(p.key, p.value)
		}
		req.URL.RawQuery = q.Encode()
	}
}

func (c *Client) methodStaticParamURL(method, pth string, params []urlParam, obj interface{}) error {
	if c.state != STATE_AUTHED {
		return ErrNoLogin
	}
	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, pth)
	req, err := http.NewRequest(method, uri, nil)
	if err != nil {
		return err
	}
	addParams(req, params)

	return c.staticRequest(req, obj, nil, params)
}

func respOk(rcode int, okCodes ...int) bool {
	for _, c := range okCodes {
		if rcode == c {
			return true
		}
	}
	return false
}

func (c *Client) staticRequest(req *http.Request, obj interface{}, okResponses []int, params []urlParam) error {
	if c.state != STATE_AUTHED {
		return ErrNoLogin
	}
	c.hm.populateRequest(req.Header) // add in the headers

	// add in any queries like ?admin=true
	var err error
	if req.URL.RawQuery, err = c.qm.appendEncode(req.URL.RawQuery); err != nil {
		return err
	}

	if len(params) > 0 {
		q := req.URL.Query()
		for _, v := range params {
			q.Add(v.key, v.value)
		}
		req.URL.RawQuery = q.Encode()
	}
	resp, err := c.clnt.Do(req)
	if err != nil {
		c.objLog.Log("WEB "+req.Method+" Error "+err.Error(), req.URL.String(), nil)
		return err
	}
	if resp == nil {
		return errors.New("Invalid response")
	}
	defer drainResponse(resp)
	if resp.StatusCode == http.StatusUnauthorized {
		c.state = STATE_LOGGED_OFF
		return ErrNotAuthed
	} else if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}

	statOk := respOk(resp.StatusCode, okResponses...)
	//either its in the list, or the list is empty and StatusOK is implied
	if !(statOk || (resp.StatusCode == http.StatusOK && len(okResponses) == 0)) {
		c.objLog.Log("WEB "+req.Method, req.URL.String()+" "+resp.Status, nil)
		return &ClientError{resp.Status, resp.StatusCode, getBodyErr(resp.Body)}
	}

	if obj != nil {
		if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
			return err
		}
	}

	c.objLog.Log("WEB "+req.Method, req.URL.String(), obj)
	return nil
}

func (c *Client) methodStaticPushRawURL(method, url string, data []byte, recvObj interface{}, okResps []int, params []urlParam) error {
	var err error

	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, url)
	req, err := http.NewRequest(method, uri, bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	c.hm.populateRequest(req.Header) // add in the headers

	// add in any queries like ?admin=true
	if req.URL.RawQuery, err = c.qm.appendEncode(req.URL.RawQuery); err != nil {
		return err
	}
	addParams(req, params)

	c.objLog.Log("WEB REQ RAW"+method, url, nil)
	resp, err := c.clnt.Do(req)
	if err != nil {
		c.objLog.Log("WEB "+method+" Error "+err.Error(), url, nil)
		return err
	}
	if resp == nil {
		return errors.New("Invalid response")
	}
	defer drainResponse(resp)
	if resp.StatusCode == http.StatusUnauthorized {
		c.state = STATE_LOGGED_OFF
		return ErrNotAuthed
	}
	if resp.StatusCode != http.StatusOK && !respOk(resp.StatusCode, okResps...) {
		c.objLog.Log("WEB "+method, url+" "+resp.Status, nil)
		return &ClientError{resp.Status, resp.StatusCode, getBodyErr(resp.Body)}
	}

	if recvObj != nil {
		if err := json.NewDecoder(resp.Body).Decode(&recvObj); err != nil {
			return err
		}
	}
	c.objLog.Log("WEB RECV", url, recvObj)
	return nil
}

func (c *Client) methodStaticPushURL(method, url string, sendObj, recvObj interface{}, okResps []int, params []urlParam) error {
	var jsonBytes []byte
	var err error

	if sendObj != nil {
		jsonBytes, err = json.Marshal(sendObj)
		if err != nil {
			return err
		}
	}
	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, url)
	req, err := http.NewRequest(method, uri, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	c.hm.populateRequest(req.Header) // add in the headers

	// add in any queries like ?admin=true
	if req.URL.RawQuery, err = c.qm.appendEncode(req.URL.RawQuery); err != nil {
		return err
	}
	addParams(req, params)

	c.objLog.Log("WEB REQ "+method, url, sendObj)
	resp, err := c.clnt.Do(req)
	if err != nil {
		c.objLog.Log("WEB "+method+" Error "+err.Error(), url, nil)
		return err
	}
	if resp == nil {
		return errors.New("Invalid response")
	}
	defer drainResponse(resp)
	if resp.StatusCode == http.StatusUnauthorized {
		c.state = STATE_LOGGED_OFF
		return ErrNotAuthed
	}
	if resp.StatusCode != http.StatusOK && !respOk(resp.StatusCode, okResps...) {
		c.objLog.Log("WEB "+method, url+" "+resp.Status, nil)
		return &ClientError{resp.Status, resp.StatusCode, getBodyErr(resp.Body)}
	}

	if recvObj != nil {
		if err := json.NewDecoder(resp.Body).Decode(&recvObj); err != nil {
			return err
		}
	}
	c.objLog.Log("WEB RECV", url, recvObj)
	return nil
}

// SearchDownloadRequest initiates a download of search results. The id parameter specifies
// the search to download. The format should be a supported download format for the search's
// renderer ("json", "csv", "text", "pcap", "lookupdata", "ipexist", "archive"). The tr
// parameter is the time frame over which results should be downloaded.
func (c *Client) SearchDownloadRequest(id, format string, tr types.TimeRange) (resp *http.Response, err error) {
	return c.SearchDownloadRequestWithContext(id, format, tr, context.TODO())
}

// SearchDownloadRequestWithContext initiates a download of search results. The id parameter specifies
// the search to download. The format should be a supported download format for the search's
// renderer ("json", "csv", "text", "pcap", "lookupdata", "ipexist", "archive"). The tr
// parameter is the time frame over which results should be downloaded.
func (c *Client) SearchDownloadRequestWithContext(id, format string, tr types.TimeRange, ctx context.Context) (resp *http.Response, err error) {
	var data []byte
	var req *http.Request
	if !tr.IsEmpty() {
		if data, err = json.Marshal(tr); err != nil {
			return
		}
	}

	var u *url.URL
	if u, err = url.Parse(searchCtrlDownloadUrl(id, format)); err != nil {
		return
	}
	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, u.String())
	if req, err = http.NewRequestWithContext(ctx, http.MethodGet, uri, bytes.NewBuffer(data)); err != nil {
		return
	}

	c.hm.populateRequest(req.Header) // add in the headers
	// add in any queries like ?admin=true
	if req.URL.RawQuery, err = c.qm.appendEncode(req.URL.RawQuery); err != nil {
		return
	}

	resp, err = c.clnt.Do(req)
	if err == nil {
		c.objLog.Log("GET "+resp.Status, u.String(), nil)
	}
	return
}

// DownloadRequest performs an authenticated GET request on the specified URL
// and hands back the http.Response object for the request.
func (c *Client) DownloadRequest(url string) (resp *http.Response, err error) {
	return c.DownloadRequestWithContext(url, context.TODO())
}

// DownloadRequestWithContext performs an authenticated GET request on the specified URL
// and hands back the http.Response object for the request.
func (c *Client) DownloadRequestWithContext(url string, ctx context.Context) (resp *http.Response, err error) {
	var req *http.Request
	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, url)
	if req, err = http.NewRequestWithContext(ctx, http.MethodGet, uri, nil); err != nil {
		return
	}

	c.hm.populateRequest(req.Header) // add in the headers

	// add in any queries like ?admin=true
	if req.URL.RawQuery, err = c.qm.appendEncode(req.URL.RawQuery); err != nil {
		return
	}

	resp, err = c.clnt.Do(req)
	if err == nil {
		c.objLog.Log("GET "+resp.Status, url, nil)
	}
	return
}

func (c *Client) methodRequestURL(method, url, contentType string, body io.Reader) (resp *http.Response, err error) {
	var req *http.Request
	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, url)
	if req, err = http.NewRequest(method, uri, body); err != nil {
		return
	}
	c.hm.populateRequest(req.Header) // add in the headers

	// add in any queries like ?admin=true
	if req.URL.RawQuery, err = c.qm.appendEncode(req.URL.RawQuery); err != nil {
		return
	}

	if contentType != `` {
		req.Header.Set("Content-Type", contentType)
	}
	if resp, err = c.clnt.Do(req); err == nil {
		c.objLog.Log(method+" "+resp.Status, url, nil)
	} else {
		c.objLog.Log(method+" "+err.Error(), uri, nil)
	}
	return
}

func (c *Client) methodParamRequestURL(method, uri string, params map[string]string, body io.Writer) (resp *http.Response, err error) {
	var req *http.Request
	if req, err = http.NewRequest(method, fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, uri), nil); err != nil {
		return
	}
	c.hm.populateRequest(req.Header) // add in the headers

	var vals url.Values
	if vals, err = url.ParseQuery(c.qm.encode()); err != nil {
		return
	}
	// add in any queries like ?admin=true
	for k, v := range params {
		vals.Add(k, v)
	}
	req.URL.RawQuery = vals.Encode()
	if resp, err = c.clnt.Do(req); err == nil {
		c.objLog.Log(method+" "+resp.Status, uri, nil)
	} else {
		c.objLog.Log(method+" "+err.Error(), uri, nil)
	}
	return
}

// GetSystemDescriptions hits the static page to hand back system descriptions
// for all active indexers and the webserver.
func (c *Client) GetSystemDescriptions() (map[string]types.SysInfo, error) {
	desc := make(map[string]types.SysInfo, 1)
	if err := c.getStaticURL(DESC_URL, &desc); err != nil {
		return nil, err
	}
	return desc, nil
}

// GetPingStates gets the connected/disconnected state of each indexer.
func (c *Client) GetPingStates() (map[string]string, error) {
	states := make(map[string]string, 1)
	if err := c.getStaticURL(STATE_URL, &states); err != nil {
		return nil, err
	}
	return states, nil
}

// GetSystemStats gets the system statistics from each active indexer.
func (c *Client) GetSystemStats() (map[string]types.SysStats, error) {
	stats := make(map[string]types.SysStats, 1)
	if err := c.getStaticURL(STATS_URL, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// GetIndexStats gets statistics for all the indexes on all connected indexers.
func (c *Client) GetIndexStats() (map[string]types.IdxStats, error) {
	stats := make(map[string]types.IdxStats, 1)
	if err := c.getStaticURL(IDX_URL, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// GetIngesterStats gets statistics for all ingesters tied to each indexer.
func (c *Client) GetIngesterStats() (map[string]types.IngestStats, error) {
	stats := map[string]types.IngestStats{}
	if err := c.getStaticURL(INGESTER_URL, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// GetStorageStats gets storage statistics for all indexers.
func (c *Client) GetStorageStats() (map[string]types.StorageStats, error) {
	stats := map[string]types.StorageStats{}
	if err := c.getStaticURL(STORAGE_URL, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// GetIndexerStorageStats gets storage statistics for the given indexer..
func (c *Client) GetIndexerStorageStats(indexer uuid.UUID) (map[string]types.PerWellStorageStats, error) {
	stats := map[string]types.PerWellStorageStats{}
	url := fmt.Sprintf(STORAGE_INDEXER_URL, indexer.String())
	if err := c.getStaticURL(url, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// GetCalendarStats gets day-by-day calendar statistics for the given wells.
func (c *Client) GetCalendarStats(start, end time.Time, wells []string) ([]types.CalendarEntry, error) {
	var stats []types.CalendarEntry

	obj := types.CalendarRequest{
		Start: start,
		End:   end,
		Wells: wells,
	}

	err := c.postStaticURL(CALENDAR_URL, obj, &stats)
	return stats, err
}

// GetIndexerCalendarStats gets day-by-day calendar statistics for a given indexer and given wells.
func (c *Client) GetIndexerCalendarStats(indexer uuid.UUID, start, end time.Time, wells []string) ([]types.CalendarEntry, error) {
	var stats []types.CalendarEntry
	obj := types.CalendarRequest{
		Start: start,
		End:   end,
		Wells: wells,
	}
	url := fmt.Sprintf(CALENDAR_INDEXER_URL, indexer.String())
	err := c.postStaticURL(url, obj, &stats)
	return stats, err
}

// GetUserList gets a listing of users with basic info like UID, name, email, etc.
func (c *Client) GetUserList() ([]types.UserDetails, error) {
	det := []types.UserDetails{}
	if err := c.getStaticURL(USERS_LIST_URL, &det); err != nil {
		return nil, err
	}
	return det, nil
}

// LookupUser looks up a UserDetails object given a username
// if the username is not found, ErrNotFound is returned
func (c *Client) LookupUser(username string) (ud types.UserDetails, err error) {
	var lst []types.UserDetails
	if lst, err = c.GetUserList(); err != nil {
		return
	}
	for _, l := range lst {
		if l.User == username {
			ud = l
			return
		}
	}

	err = ErrNotFound
	return
}

// GetGroupList gets a listing of groups with basic info like GID, name, desc.
func (c *Client) GetGroupList() ([]types.GroupDetails, error) {
	det := []types.GroupDetails{}
	if err := c.getStaticURL(GROUP_URL, &det); err != nil {
		return nil, err
	}
	return det, nil
}

// LookupGroup looks up a GroupDetails object given a group name
// if the group name is not found, ErrNotFound is returned
func (c *Client) LookupGroup(groupname string) (gd types.GroupDetails, err error) {
	var lst []types.GroupDetails
	if lst, err = c.GetGroupList(); err != nil {
		return
	}
	for _, l := range lst {
		if l.Name == groupname {
			gd = l
			return
		}
	}

	err = ErrNotFound
	return
}

// a test get without locking. For internal calls
func (c *Client) nolockTestGet(path string) error {
	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, path)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return err
	}
	c.hm.populateRequest(req.Header) // add in the headers

	// add in any queries like ?admin=true
	if req.URL.RawQuery, err = c.qm.appendEncode(req.URL.RawQuery); err != nil {
		return err
	}

	resp, err := c.clnt.Do(req)
	if err != nil {
		return err
	}
	if resp == nil {
		return errors.New("Invalid response")
	}
	defer drainResponse(resp)
	if resp.StatusCode == http.StatusUnauthorized {
		c.state = STATE_LOGGED_OFF
		return errors.New("Test GET returned StatusUnauthorized")
	}
	if resp.StatusCode != http.StatusOK {
		return &ClientError{resp.Status, resp.StatusCode, getBodyErr(resp.Body)}
	}

	return nil
}

func (c *Client) uploadMultipartFile(url, field, name string, rdr io.Reader, fields map[string]string) (resp *http.Response, err error) {
	return c.uploadMultipartFileMethod(http.MethodPost, url, field, name, rdr, fields)
}

func (c *Client) uploadMultipartFileMethod(method, url, field, name string, rdr io.Reader, fields map[string]string) (resp *http.Response, err error) {
	r, w := io.Pipe()
	rch := make(chan error, 1)
	defer close(rch)
	wtr := multipart.NewWriter(w)

	go func(wtr *multipart.Writer, w io.WriteCloser, ch chan error) {
		//write the field parameters
		for k, v := range fields {
			if err := wtr.WriteField(k, v); err != nil {
				w.Close()
				wtr.Close()
				ch <- err
				return
			}
		}
		//write the file portion (the name is ignored)
		if part, err := wtr.CreateFormFile(field, name); err != nil {
			w.Close()
			wtr.Close()
			ch <- err
			return
		} else if _, err := io.Copy(part, rdr); err != nil {
			w.Close()
			wtr.Close()
			ch <- err
			return
		} else if err := wtr.Close(); err != nil {
			w.Close()
			ch <- err
		} else if err := w.Close(); err != nil {
			ch <- err
		} else {
			ch <- nil
		}
	}(wtr, w, rch)

	if resp, err = c.methodRequestURL(method, url, wtr.FormDataContentType(), r); err != nil {
		r.Close()
		<-rch
		return
	}
	if err = <-rch; err != nil {
		return
	} else if err = r.Close(); err != nil {
		return
	}
	return
}

// getBodyErr pulls a possible error message out of the response body
// and returns it as a string.  We will yank a maximum of 256 bytes
func getBodyErr(rc io.Reader) string {
	resp := make([]byte, 256)
	n, err := rc.Read(resp)
	if (err != nil && err != io.EOF) || n <= 0 {
		return ""
	}
	return strings.TrimSpace(string(resp[0:n]))
}

type bodyErr struct {
	Error string
}

func decodeBodyError(rdr io.Reader) error {
	var be bodyErr
	if rdr == nil {
		return nil
	}
	err := json.NewDecoder(rdr).Decode(&be)
	if err == nil {
		return errors.New(be.Error)
	} else if err == io.EOF {
		return nil
	}
	return err
}
