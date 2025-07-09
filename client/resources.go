/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/gravwell/gravwell/v3/client/types"
)

// GetResourceList returns information about all resources the user can access.
func (c *Client) GetResourceList() (rm []types.ResourceMetadata, err error) {
	if err = c.getStaticURL(resourcesUrl(), &rm); err != nil {
		rm = nil
	}
	return
}

// GetAllResourceList is an admin-only API to pull back the entire resource list.
// Non-administrators will receive the same list as returned by GetResourceList.
func (c *Client) GetAllResourceList() (rm []types.ResourceMetadata, err error) {
	c.SetAdminMode()
	if err = c.getStaticURL(resourcesUrl(), &rm); err != nil {
		rm = nil
	}
	c.ClearAdminMode()

	return
}

// CreateResource makes a new resource. The resource name and description are specified at
// creation time, as are the Global flag and an optional list of groups with which to
// share it. The return value contains information about the newly-created resource.
func (c *Client) CreateResource(name, description string, global bool, groups []int32) (*types.ResourceMetadata, error) {
	spec := types.ResourceMetadata{
		ResourceName: name,
		Description:  description,
		Global:       global,
		GroupACL:     groups,
	}
	var resp types.ResourceMetadata
	if err := c.postStaticURL(resourcesUrl(), spec, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// PopulateResource updates the contents of the resource with the specified GUID.
func (c *Client) PopulateResource(guid string, data []byte) error {
	//	return c.putStaticRawURL(resourcesGuidRawUrl(guid), data)
	return c.PopulateResourceFromReader(guid, bytes.NewReader(data))
}

type mpWriter struct {
	*multipart.Writer
	bio *bufio.Writer
	pw  *io.PipeWriter
}

func newMpWriter(w *io.PipeWriter) *mpWriter {
	bio := bufio.NewWriter(w)
	mp := multipart.NewWriter(bio)
	return &mpWriter{
		Writer: mp,
		bio:    bio,
		pw:     w,
	}
}

func (mpw *mpWriter) Close() (err error) {
	if err = mpw.Writer.Close(); err == nil {
		if err = mpw.bio.Flush(); err == nil {
			err = mpw.pw.Close()
		} else {
			mpw.pw.CloseWithError(err)
		}
	} else {
		mpw.pw.CloseWithError(err)
	}
	return
}

// PopulateResourceFromReader updates the contents of the specified resource using
// data read from an io.Reader rather than a slice of bytes.
func (c *Client) PopulateResourceFromReader(guid string, data io.Reader) (err error) {
	var part io.Writer
	var resp *http.Response

	//get a pipe rolling with something that always closes it
	rdr, wtr := io.Pipe()
	defer wtr.Close()
	defer rdr.Close()

	mpw := newMpWriter(wtr)
	//write the file portion (the name is ignored)
	if part, err = mpw.CreateFormFile(userFileField, `file`); err != nil {
		return
	}
	contentType := mpw.FormDataContentType()

	go func() {
		//perform the copy, any read errors are shoved into the writer so the reader gets them too
		if _, lerr := io.Copy(part, data); lerr != nil {
			wtr.CloseWithError(lerr)
		}

		if lerr := mpw.Close(); lerr != nil {
			wtr.CloseWithError(lerr)
		}
	}()

	resp, err = c.methodRequestURL(http.MethodPut, resourcesGuidRawUrl(guid), contentType, rdr)
	if err != nil {
		return err
	}
	defer drainResponse(resp)

	if resp.StatusCode == http.StatusUnauthorized {
		c.state = STATE_LOGGED_OFF
		err = ErrNotAuthed
	} else if resp.StatusCode != http.StatusOK {
		if s := getBodyErr(resp.Body); len(s) > 0 {
			err = errors.New(s)
		} else {
			err = fmt.Errorf("Bad Status %s(%d)", resp.Status, resp.StatusCode)
		}
	}

	return
}

// DeleteResource removes a resource by GUID.
func (c *Client) DeleteResource(guid string) error {
	return c.deleteStaticURL(resourcesGuidUrl(guid), nil)
}

// UpdateMetadata sets the specified resource's metadata.
func (c *Client) UpdateMetadata(guid string, metadata types.ResourceMetadata) error {
	return c.putStaticURL(resourcesGuidUrl(guid), metadata)
}

// GetResourceMetadata gets the specified resource's metadata.
func (c *Client) GetResourceMetadata(guid string) (*types.ResourceMetadata, error) {
	var metadata types.ResourceMetadata
	err := c.getStaticURL(resourcesGuidUrl(guid), &metadata)
	return &metadata, err
}

// GetResource returns the contents of the resource with the specified name. The
// name can be either the user-friendly Name field, or a stringified GUID. Because
// resources can be shared, and resources are not required to have globally-unique names,
// the following precedence is used when selecting a resource by user-friendly name:
// 1. Resources owned by the user always have highest priority
// 2. Resources shared with a group to which the user belongs are next
// 3. Global resources are the lowest priority
func (c *Client) GetResource(name string) ([]byte, error) {
	var guid string
	err := c.getStaticURL(resourcesLookupUrl(name), &guid)
	if err != nil {
		return nil, err
	}

	resp, err := c.methodRequestURL(http.MethodGet, resourcesGuidRawUrl(guid), ``, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// LookupResourceGUID attempts to resolve the GUID for a resource with the specified
// user-friendly name. It follows precedence as defined on the GetResource method.
func (c *Client) LookupResourceGUID(name string) (string, error) {
	var guid string
	err := c.getStaticURL(resourcesLookupUrl(name), &guid)
	if err != nil {
		return "", err
	}
	return guid, nil
}

// CloneResource creates a copy of an existing resource (specified by GUID) with the
// Name field set to the newName parameter.
func (c *Client) CloneResource(guid string, newName string) (*types.ResourceMetadata, error) {
	spec := struct{ Name string }{
		Name: newName,
	}
	var resp types.ResourceMetadata
	if err := c.postStaticURL(resourcesCloneUrl(guid), spec, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
