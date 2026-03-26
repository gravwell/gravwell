/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
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
	"net/http"

	"github.com/gravwell/gravwell/v4/client/types"
)

// CleanupResources (admin-only) purges all deleted resources for all users.
func (c *Client) CleanupFiles() error {
	return c.deleteStaticURL(filesUrl(), nil)
}

// CreateFile makes a new file.
// The name and description are specified at creation time,
// as are the Global flag and an optional list of groups with which to share it.
//
// The files contents may optionally be included.
//
// The return value contains information about the newly-created file.
func (c *Client) CreateFile(f types.File, data []byte) (result types.File, err error) {
	c.CreateFileFromReader()
}

// PopulateFileFromReader performs CreateFile, but takes a reader instead of raw bytes.
func (c *Client) CreateFileFromReader(f types.File, data io.Reader) error {
	// TODO
}

// UpdateFileMetadata sets the specified file's metadata.
//
// Changes to ID, size, and/or hash will be ignored.
func (c *Client) UpdateFileMetadata(id string, metadata types.File) (err error) {
	return c.putStaticURL(filesIdUrl(id), metadata)
}

// GetFileMetadata gets the specified file's metadata.
func (c *Client) GetFileMetadata(id string) (types.File, error) {
	var metadata types.File
	err := c.getStaticURL(filesIdUrl(id), &metadata)
	return metadata, err
}

// GetFile returns the contents of the file with the specified ID.
func (c *Client) GetFile(id string) ([]byte, error) {
	var meta types.File

	resp, err := c.methodRequestURL(http.MethodGet, filesIdRawUrl(meta.ID), ``, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// PopulateFile updates the contents of the specified file.
// This will update the hash and size fields of the file's metadata.
func (c *Client) PopulateFile(id string, data []byte) error {
	return c.PopulateFileFromReader(id, bytes.NewReader(data))
}

func (c *Client) PopulateFileFromReader(id string, data io.Reader) error {
	var part io.Writer
	var resp *http.Response

	//get a pipe rolling with something that always closes it
	rdr, wtr := io.Pipe()
	defer wtr.Close()
	defer rdr.Close()

	mpw := newMpWriter(wtr)
	//write the file portion (the name is ignored)
	part, err := mpw.CreateFormFile(userFileField, `file`)
	if err != nil {
		return err
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

	resp, err = c.methodRequestURL(http.MethodPut, filesIdRawUrl(id), contentType, rdr)
	if err != nil {
		return err
	}
	defer drainResponse(resp)

	if resp.StatusCode == http.StatusUnauthorized {
		c.state = STATE_LOGGED_OFF
		return ErrNotAuthed
	} else if resp.StatusCode != http.StatusOK {
		if s := getBodyErr(resp.Body); len(s) > 0 {
			return errors.New(s)
		}
		return fmt.Errorf("Bad Status %s(%d)", resp.Status, resp.StatusCode)
	}

	return nil
}

// ListFiles returns information about all files the user can access
func (c *Client) ListFiles(opts *types.QueryOptions) (ret types.FileListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(FILES_LIST_URL, opts, &ret)
	return
}

// ListAllFiles is an admin-only API to pull back the entire file list.
// Non-administrators will receive the same list as returned by ListFiles.
func (c *Client) ListAllFiles(opts *types.QueryOptions) (ret types.FileListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true
	err = c.postStaticURL(FILES_LIST_URL, opts, &ret)
	return
}

// DeleteFile removes a file by ID by marking it deleted in the database.
func (c *Client) DeleteFile(id string) error {
	return c.deleteStaticURL(filesIdUrl(id), nil)
}

// PurgeFile removes the specified ID entirely, skipping any kind of soft-delete.
func (c *Client) PurgeFile(id string) error {
	return c.deleteStaticURL(filesIdUrl(id), nil, ezParam("purge", "true"))

}
