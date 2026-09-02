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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/ingest"
)

const (
	// Form header that contents are attached under
	fileField   string = "file"
	maxFileSize uint64 = 8 * 1024 * 1024
)

var ErrOversizedFile error = fmt.Errorf("Files must be %v or smaller", ingest.HumanSize(maxFileSize))

// CleanupFiles (admin-only) purges all deleted files for all users.
func (c *Client) CleanupFiles() error {
	return c.cleanup(filesUrl())
}

// CreateFile makes a new file.
// The name and description are specified at creation time,
// as are the Global flag and an optional list of groups with which to share it.
//
// The return value contains information about the newly-created file.
func (c *Client) CreateFile(f types.File) (result types.File, err error) {
	err = c.postStaticURL(filesUrl(), f, &result)
	return result, err
}

// GetFile returns the specified file's contents.
func (c *Client) GetFile(id string) ([]byte, error) {
	return c.GetFileEx(id, nil, 0)
}

// GetFileEx returns the specified file's contents.
// If opts is not nil, applicable parameters (currently only IncludeDeleted) will be applied to the query.
// Up to previewBytes will be returned; if 0, everything is returned.
func (c *Client) GetFileEx(id string, opts *types.QueryOptions, previewBytes uint64) ([]byte, error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}

	resp, err := c.methodParamRequestURL(http.MethodGet, filesIdRawUrl(id), map[string]string{
		"include_deleted": strconv.FormatBool(opts.IncludeDeleted),
		"bytes":           strconv.FormatUint(previewBytes, 10),
	})
	if err != nil {
		return nil, err
	} else if err := aliasResponseError(c, resp); err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// UpdateFileMetadata modifies an existing file's metadata and returns the complete, updated struct.
func (c *Client) UpdateFileMetadata(ID string, p types.FilePatch) (updated types.File, err error) {
	if ID == "" {
		return types.File{}, ErrEmptyID
	}
	return c.patch[types.FilePatch, types.File](filesIdUrl(ID), p)
}

// GetFileMetadata gets the specified file sans contents.
func (c *Client) GetFileMetadata(id string) (types.File, error) {
	var metadata types.File
	err := c.getStaticURL(filesIdUrl(id), &metadata)
	return metadata, err
}

// PopulateFile sets the content of the specified file to the given data.
//
// Extension should include the dot (ex: ".csv").
//
// Returns the metadata of the populated/updated file.
func (c *Client) PopulateFile(id string, extension string, data []byte) (types.File, error) {
	if uint64(len(data)) > maxFileSize {
		return types.File{}, ErrOversizedFile
	}
	return c.PopulateFileFromReader(id, extension, bytes.NewBuffer(data))
}

// PopulateFileFromPath sets the content of the specified file to that of the file at the given path.
// Extension is taken verbatim from the path.
//
// Returns the metadata of the populated/updated file.
func (c *Client) PopulateFileFromPath(id string, pth string) (types.File, error) {
	f, err := os.Open(pth)
	if err != nil {
		return types.File{}, err
	}

	// sanity check the length
	if fi, err := f.Stat(); err != nil {
		return types.File{}, err
	} else if uint64(fi.Size()) > maxFileSize {
		return types.File{}, ErrOversizedFile
	}

	return c.PopulateFileFromReader(id, filepath.Ext(pth), f)
}

// PopulateFileFromReader sets the contents of the specified file to that of the given reader.
//
// Extension should include the dot (ex: ".csv").
//
// Returns the metadata of the populated/updated file.
func (c *Client) PopulateFileFromReader(id string, extension string, data io.Reader) (types.File, error) {
	var resp *http.Response

	//get a pipe rolling with something that always closes it
	rdr, wtr := io.Pipe()
	defer wtr.Close()
	defer rdr.Close()

	mpw := newMpWriter(wtr)
	//write the file portion; we only care about the extension as name is stored in metadata.
	part, err := mpw.CreateFormFile(fileField, `file`+extension)
	if err != nil {
		return types.File{}, err
	}
	contentType := mpw.FormDataContentType()

	go func() {
		// Perform the copy, but read one extra byte so oversized payloads are detected
		// instead of being silently truncated at maxFileSize.
		lr := io.LimitReader(data, int64(maxFileSize)+1)
		n, lerr := io.Copy(part, lr)
		if lerr != nil {
			wtr.CloseWithError(lerr)
			return
		}
		if uint64(n) > maxFileSize {
			wtr.CloseWithError(ErrOversizedFile)
			return
		}

		if lerr := mpw.Close(); lerr != nil {
			wtr.CloseWithError(lerr)
		}
	}()

	resp, err = c.methodRequestURL(http.MethodPut, filesIdRawUrl(id), contentType, rdr)
	if err != nil {
		return types.File{}, err
	}

	if err := aliasResponseError(c, resp); err != nil {
		return types.File{}, err
	}
	defer drainResponse(resp)

	// decode the metadata response
	confirmation := types.File{}
	if err := json.NewDecoder(resp.Body).Decode(&confirmation); err != nil {
		return types.File{}, err
	}

	return confirmation, nil
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
