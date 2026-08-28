package client

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"net/http"

	"github.com/gravwell/gravwell/v4/client/types"
)

// PATCHRequest submits a PATCH request against the given url.
func (c *Client) PATCHRequest[PatchT types.PatchType, ResponseT any](url string, data PatchT) (patched ResponseT, _ error) {
	body, err := json.Marshal(data, json.OmitZeroStructFields(true))
	if err != nil {
		return patched, err
	} else if body == nil || string(body) == "{}" { // if this marshaled to no data, throw away the request
		return patched, ErrEmptyPatch
	}

	resp, err := c.reqDriver(http.MethodPatch, url, body)
	defer drainResponse(resp)
	if err != nil {
		return patched, err
	}

	if err := json.UnmarshalRead(resp.Body, &patched); err != nil {
		return patched, err
	}

	c.objLog.Log("WEB RECV", url, patched)
	return patched, nil
}

// TODO annotate
func (c *Client) CleanupRequest(url string) error {
	resp, err := c.reqDriver(http.MethodDelete, url, nil)
	defer drainResponse(resp)
	return err
}

// reqDriver powers outbound requests and serves as a funnel to keep them consistent.
// If err == nil, the caller is responsible for draining the response.
func (c *Client) reqDriver(method string, url string, body []byte) (*http.Response, error) {
	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, url)
	req, err := http.NewRequest(method, uri, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	c.hm.populateRequest(req.Header) // add in the headers

	// add in any queries like ?admin=true
	if req.URL.RawQuery, err = c.qm.appendEncode(req.URL.RawQuery); err != nil {
		return nil, err
	}

	c.objLog.Log("WEB REQ "+req.Method, url, string(body))
	resp, err := c.clnt.Do(req)
	if err != nil {
		c.objLog.Log("WEB "+req.Method+" Error "+err.Error(), url, nil)
		drainResponse(resp)
		return nil, err
	}
	if resp == nil {
		return nil, ErrNilResponse
	}
	if resp.StatusCode != http.StatusOK {
		c.objLog.Log("WEB "+req.Method, url+" "+resp.Status, nil)
		defer drainResponse(resp)
		return nil, aliasResponseError(c, resp)
	}
	return resp, nil
}
