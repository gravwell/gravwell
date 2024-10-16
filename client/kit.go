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
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"

	"github.com/gravwell/gravwell/v4/client/types"

	"github.com/google/uuid"
)

const (
	minKitSize int64 = 128
)

var (
	ErrInvalidKitSize = errors.New("Kit is too small to upload")
)

// UploadKit stages a kit file for installation. The parameter 'p' should
// be the path of a kit file on disk. A KitState object containing information
// about the kit is returned on success.
func (c *Client) UploadKit(p string) (pc types.KitState, err error) {
	var fin *os.File
	var fi os.FileInfo
	var mp io.Writer
	var req *http.Request
	//open the file
	if fin, err = os.Open(p); err != nil {
		return
	}
	if fi, err = fin.Stat(); err != nil {
		fin.Close()
		return
	}
	if fi.Size() <= 0 {
		fin.Close()
		err = errors.New("License file is empty")
		return
	}

	bb := bytes.NewBuffer(nil)
	wtr := multipart.NewWriter(bb)
	if mp, err = wtr.CreateFormFile(`file`, `package`); err != nil {
		fin.Close()
		return
	}

	if _, err = io.Copy(mp, fin); err != nil {
		fin.Close()
		return
	}
	if err = wtr.Close(); err != nil {
		fin.Close()
		return
	}
	if err = fin.Close(); err != nil {
		return
	}

	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, kitUrl())
	if req, err = http.NewRequest(http.MethodPost, uri, bb); err != nil {
		return
	}
	req.Header.Set(`Content-Type`, wtr.FormDataContentType())

	okResps := []int{http.StatusOK, http.StatusMultiStatus}
	err = c.staticRequest(req, &pc, okResps, nil)
	return
}

// PullKit tells the webserver to stage the kit with the specified GUID for installation,
// pulling the kit from the kit server. A KitState object containing information
// about the kit is returned on success.
func (c *Client) PullKit(guid uuid.UUID) (pc types.KitState, err error) {
	var mp io.Writer
	var req *http.Request
	bb := bytes.NewBuffer(nil)
	wtr := multipart.NewWriter(bb)
	if mp, err = wtr.CreateFormField(`remote`); err != nil {
		return
	}
	if _, err = mp.Write([]byte(guid.String())); err != nil {
		return
	}

	if err = wtr.Close(); err != nil {
		return
	}
	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, kitUrl())
	if req, err = http.NewRequest(http.MethodPost, uri, bb); err != nil {
		return
	}
	req.Header.Set(`Content-Type`, wtr.FormDataContentType())

	okResps := []int{http.StatusOK, http.StatusMultiStatus}
	err = c.staticRequest(req, &pc, okResps, nil)
	return

}

// ListRemoteKits returns a list of kits available on the kit server.
func (c *Client) ListRemoteKits(all bool) (mds []types.KitMetadata, err error) {
	err = c.getStaticURL(remoteKitUrl(all), &mds)
	return
}

// ListKits returns a list of all installed and staged kits.
func (c *Client) ListKits() (pkgs []types.IdKitState, err error) {
	err = c.getStaticURL(kitUrl(), &pkgs)
	return
}

// KitInfo returns information about a particular installed/staged kit, specified
// by the kit's UUID.
func (c *Client) KitInfo(id uuid.UUID) (ki types.IdKitState, err error) {
	err = c.getStaticURL(kitIdUrl(id.String()), &ki)
	return
}

// InstallKit tells the webserver to install a staged kit. The id parameter
// is the UUID of the staged kit. The cfg parameter provides install-time
// options.
func (c *Client) InstallKit(id string, cfg types.KitConfig) (err error) {
	err = c.putStaticURL(kitIdUrl(id), cfg)
	return
}

// ModifyKit tells the webserver to change parameters on an installed kit.
// The id parameter is the UUID of the installed kit. The cfg parameter provides
// the desired changes, with the following fields being respected: Global, InstallationGroup,
// and Labels.
func (c *Client) ModifyKit(id string, cfg types.KitConfig) (report types.KitModifyReport, err error) {
	err = c.methodStaticPushURL(http.MethodPatch, kitIdUrl(id), cfg, &report, nil, nil)
	return
}

// DeleteKit uninstalls a kit (specified by UUID). Note that if kit items
// have been modified, DeleteKit will return an error; use ForceDeleteKit to
// remove the kit regardless.
func (c *Client) DeleteKit(id string) (err error) {
	err = c.deleteStaticURL(kitIdUrl(id), nil)
	return
}

// DeleteKitEx attempts to uninstall a kit. If kit items have been modified,
// it will return an error and a list of modified items. If nothing has been
// changed, it returns an empty list and a nil error.
func (c *Client) DeleteKitEx(id string) ([]types.SourcedKitItem, error) {
	var resp *http.Response
	var err error
	resp, err = c.methodRequestURL(http.MethodDelete, kitIdUrl(id), ``, nil)
	if err != nil {
		// this means we weren't able to get a request to the server, return the error
		return []types.SourcedKitItem{}, err
	}
	defer drainResponse(resp)
	if resp.StatusCode != http.StatusOK {
		// There are basically two kinds of errors:
		// 1. Kit items have been modified; body contains a list of modified items
		// 2. Other errors (kit doesn't exist, malformed ID, etc.)
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return []types.SourcedKitItem{}, err
		}
		var ks struct {
			ModifiedItems []types.SourcedKitItem
			Error         string
		}
		if err := json.Unmarshal(body, &ks); err != nil {
			// This was a type 2 error: "something else"
			return []types.SourcedKitItem{}, fmt.Errorf("Bad status %v: %v", resp.Status, string(body))
		}
		return ks.ModifiedItems, errors.New(ks.Error)
	}
	// Success, the kit should be deleted now.
	return []types.SourcedKitItem{}, nil
}

// AdminDeleteKit is an admin-only function which can delete a kit owned by
// any user.
func (c *Client) AdminDeleteKit(id string) (err error) {
	c.SetAdminMode()
	err = c.deleteStaticURL(kitIdUrl(id), nil)
	c.ClearAdminMode()

	return
}

// ForceDeleteKit uninstalls a kit (specified by UUID) regardless of any
// changes made since installation.
func (c *Client) ForceDeleteKit(id string) (err error) {
	params := []urlParam{
		urlParam{key: "force", value: "true"},
	}
	err = c.methodStaticParamURL(http.MethodDelete, kitIdUrl(id), params, nil)
	return
}

// BuildKit builds a new kit. The parameter 'pbr' contains information about
// the kit to be built, including lists of objects to include. On success, the
// returned KitBuildResponse will contain a UUID which can be used to download
// the kit via the KitDownloadRequest function.
func (c *Client) BuildKit(pbr types.KitBuildRequest) (r types.KitBuildResponse, err error) {
	err = c.postStaticURL(kitBuildUrl(), pbr, &r)
	return
}

// DeleteBuildKit removes a recently-built kit.
func (c *Client) DeleteBuildKit(id string) (err error) {
	err = c.deleteStaticURL(kitDownloadUrl(id), nil)
	return
}

// KitDownloadRequest initiates a download for the specified kit and returns
// the associated http.Response structure. The kit is available in the Body
// field of the response.
func (c *Client) KitDownloadRequest(id string) (*http.Response, error) {
	return c.DownloadRequest(kitDownloadUrl(id))
}

// AdminListKits is an admin-only function which lists all kits on the system.
// Non-administrators will get the same list as returned by ListKits.
func (c *Client) AdminListKits() (pkgs []types.IdKitState, err error) {
	c.SetAdminMode()
	if err = c.getStaticURL(kitUrl(), &pkgs); err != nil {
		pkgs = nil
	}
	c.ClearAdminMode()

	return
}

// KitStatuses returns the statuses of any ongoing or completed kit installations.
func (c *Client) KitStatuses() (statuses []types.InstallStatus, err error) {
	err = c.getStaticURL(kitStatusUrl(), &statuses)
	return
}

// ListKitBuildHistory returns KitBuildRequests for all kits previously built by the
// user. Note that only the most recent build request is stored for each unique
// kit ID (e.g. "io.gravwell.foo").
func (c *Client) ListKitBuildHistory() (hist []types.KitBuildRequest, err error) {
	err = c.getStaticURL(kitBuildHistoryUrl(), &hist)
	return
}

// DeleteKitBuildHistory deletes a build history entry for the given ID e.g. "io.gravwell.foo"
func (c *Client) DeleteKitBuildHistory(id string) error {
	return c.deleteStaticURL(kitDeleteBuildHistoryUrl(id), nil)
}
