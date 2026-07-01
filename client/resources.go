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
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gravwell/gravwell/v4/client/types"
)

// CreateResource makes a new resource. The resource name and description are specified at
// creation time, as are the Global flag and an optional list of groups with which to
// share it. The return value contains information about the newly-created resource.
func (c *Client) CreateResource(r types.Resource) (types.Resource, error) {
	var resp types.Resource
	if err := c.postStaticURL(resourcesUrl(), r, &resp); err != nil {
		return resp, err
	}
	return resp, nil
}

// ListResources returns information about all resources the user can access.
func (c *Client) ListResources(opts *types.QueryOptions) (rm types.ResourceListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(RESOURCES_LIST_URL, opts, &rm)
	return
}

// ListAllResources is an admin-only API to pull back the entire resource list.
// Non-administrators will receive the same list as returned by ListResources.
func (c *Client) ListAllResources(opts *types.QueryOptions) (rm types.ResourceListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true
	err = c.postStaticURL(RESOURCES_LIST_URL, opts, &rm)

	return
}

// PopulateResource sets the content of the specified resource to the given data.
//
// Extension should include the dot (ex: ".csv").
//
// Returns the metadata of the populated/updated resource.
func (c *Client) PopulateResource(id string, extension string, data []byte) (types.Resource, error) {
	return c.PopulateResourceFromReader(id, extension, bytes.NewReader(data))
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

// PopulateResourceFromPath sets the content of the specified resource to that of the file at the given path.
// Extension is taken verbatim from the path.
//
// Returns the metadata of the populated/updated resource.
func (c *Client) PopulateResourceFromPath(id string, pth string) (types.Resource, error) {
	f, err := os.Open(pth)
	if err != nil {
		return types.Resource{}, err
	}

	return c.PopulateResourceFromReader(id, filepath.Ext(pth), f)
}

// PopulateResourceFromReader sets the contents of the specified resource to that of the given reader.
//
// Extension should include the dot (ex: ".csv").
//
// Returns the metadata of the populated/updated resource.
func (c *Client) PopulateResourceFromReader(id string, extension string, data io.Reader) (types.Resource, error) {
	var resp *http.Response

	//get a pipe rolling with something that always closes it
	rdr, wtr := io.Pipe()
	defer wtr.Close()
	defer rdr.Close()

	mpw := newMpWriter(wtr)
	//write the file portion; we only care about the extension as name is stored in metadata.
	part, err := mpw.CreateFormFile(fileField, `file`+extension)
	if err != nil {
		return types.Resource{}, err
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

	resp, err = c.methodRequestURL(http.MethodPut, resourcesIdRawUrl(id), contentType, rdr)
	if err != nil {
		return types.Resource{}, err
	} else if err := checkResponse(c, resp); err != nil {
		return types.Resource{}, err
	}

	// decode the metadata response
	confirmation := types.Resource{}
	if err := json.NewDecoder(resp.Body).Decode(&confirmation); err != nil {
		return types.Resource{}, err
	}

	return confirmation, nil
}

// DeleteResource removes a resource by ID by marking it deleted in the database.
func (c *Client) DeleteResource(id string) error {
	return c.deleteStaticURL(resourcesIdUrl(id), nil)
}

// PurgeResource removes a resource by ID entirely.
func (c *Client) PurgeResource(id string) error {
	return c.deleteStaticURL(resourcesIdUrl(id), nil, ezParam("purge", "true"))
}

// CleanupResources (admin-only) purges all deleted resources for all users.
func (c *Client) CleanupResources() error {
	return c.deleteStaticURL(RESOURCES_URL, nil)
}

// UpdateResourceMetadata sets the specified resource's metadata.
func (c *Client) UpdateResourceMetadata(id string, metadata types.Resource) (updated types.File, err error) {
	err = c.methodStaticPushURL(http.MethodPut, resourcesIdUrl(id), metadata, &updated, nil, nil)
	return updated, err
}

// GetResourceMetadata gets the specified resource's metadata.
func (c *Client) GetResourceMetadata(id string) (types.Resource, error) {
	var metadata types.Resource
	err := c.getStaticURL(resourcesIdUrl(id), &metadata)
	return metadata, err
}

// GetResource returns the contents of the resource with the specified name. The
// name can be either the user-friendly Name field, or an ID. Because
// resources can be shared, and resources are not required to have globally-unique names,
// the following precedence is used when selecting a resource by user-friendly name:
// 1. Resources owned by the user always have highest priority
// 2. Resources shared with a group to which the user belongs are next
// 3. Global resources are the lowest priority
func (c *Client) GetResource(name string) ([]byte, error) {
	return c.GetResourceEx(name, nil, 0)
}

// GetResourceEx returns the contents of the resource with the specified name, up to previewBytes (if 0, everything is returned).
// Follows the name/ID logic of GetResource.
//
// If opts is not nil, applicable parameters (currently only IncludeDeleted) will be applied to the query.
// Up to previewBytes will be returned; if 0, everything is returned.
func (c *Client) GetResourceEx(name string, opts *types.QueryOptions, previewBytes uint64) ([]byte, error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}

	var meta types.Resource
	err := c.getStaticURL(resourcesLookupUrl(name), &meta, ezParam("include_deleted", opts.IncludeDeleted))
	if err != nil {
		return nil, err
	}

	resp, err := c.methodParamRequestURL(http.MethodGet, resourcesIdRawUrl(meta.ID), map[string]string{
		"include_deleted": strconv.FormatBool(opts.IncludeDeleted),
		"bytes":           strconv.FormatUint(previewBytes, 10),
	})
	if err != nil {
		return nil, err
	} else if err := checkResponse(c, resp); err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// LookupResource attempts to resolve the resource with the specified
// user-friendly name. It follows precedence as defined on the GetResource method.
func (c *Client) LookupResource(name string) (types.Resource, error) {
	var meta types.Resource
	err := c.getStaticURL(resourcesLookupUrl(name), &meta)
	if err != nil {
		return types.Resource{}, err
	}
	return meta, nil
}

// CloneResource creates a copy of an existing resource (specified by ID) with the
// Name field set to the newName parameter.
func (c *Client) CloneResource(id string, newName string) (types.Resource, error) {
	spec := struct{ Name string }{
		Name: newName,
	}
	var resp types.Resource
	if err := c.postStaticURL(resourcesCloneUrl(id), spec, &resp); err != nil {
		return resp, err
	}
	return resp, nil
}
