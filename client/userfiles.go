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
	"net/http"
	"os"

	"github.com/gravwell/gravwell/v3/client/types"

	"github.com/google/uuid"
)

const (
	maxFileSize       int64  = 8 * 1024 * 1024
	userFileField     string = `file`
	userFileNameField string = `name`
	userFileDescField string = `desc`
	userFileMetaField string = `meta`
	userFileGuidField string = `guid`
)

var (
	ErrInvalidUserFileSize = errors.New("UserFile is too large to upload")
)

// UserFiles lists all the user files the logged in account has access to
func (c *Client) UserFiles() (ufds []types.UserFileDetails, err error) {
	err = c.getStaticURL(userFilesUrl(), &ufds)
	return
}

// AllUserFiles pulls the complete list of all user files for the entire system.
// Non-administrators will receive the same list as returned by UserFiles.
func (c *Client) AllUserFiles() (ufds []types.UserFileDetails, err error) {
	c.SetAdminMode()
	if err = c.getStaticURL(userFilesUrl(), &ufds); err != nil {
		ufds = nil
	}
	c.ClearAdminMode()
	return
}

// AddUserFile creates a new user file with the specified name and description.
// pth should point to a valid file on the local system.
func (c *Client) AddUserFile(name, desc, pth string) (guid uuid.UUID, err error) {
	var fin *os.File
	if fin, err = os.Open(pth); err != nil {
		return
	}

	meta := types.UserFileDetails{Name: name, Desc: desc}
	if guid, err = c.uploadUserFile(http.MethodPost, userFilesUrl(), fin, meta); err != nil {
		fin.Close()
		return
	}
	err = fin.Close()
	return
}

// AddUserFileDetails creates a new user file (uploaded from pth) with details set by the meta parameter.
func (c *Client) AddUserFileDetails(meta types.UserFileDetails, pth string) (guid uuid.UUID, err error) {
	var fin *os.File
	if fin, err = os.Open(pth); err != nil {
		return
	}

	if guid, err = c.uploadUserFile(http.MethodPost, userFilesUrl(), fin, meta); err != nil {
		fin.Close()
		return
	}
	err = fin.Close()
	return
}

// DeleteUserFile removes a user file by its GUID
func (c *Client) DeleteUserFile(id uuid.UUID) (err error) {
	err = c.deleteStaticURL(userFilesIdUrl(id), nil)
	return
}

// UpdateUserFile will push a new user file with name and description to the given GUID
func (c *Client) UpdateUserFile(id uuid.UUID, pth string) (err error) {
	var fin *os.File
	if fin, err = os.Open(pth); err != nil {
		return
	}
	// doesn't really matter, it is not used
	meta := types.UserFileDetails{}
	if _, err = c.uploadUserFile(http.MethodPut, userFilesIdUrl(id), fin, meta); err != nil {
		fin.Close()
		return
	}
	err = fin.Close()

	return
}

// UpdateUserFileMetadata will change every field of the user file
// but not the actual contents of the file
func (c *Client) UpdateUserFileMetadata(id uuid.UUID, uf types.UserFileDetails) (err error) {
	return c.patchStaticURL(userFilesIdUrl(id), uf)
}

// GetUserFile downloads a file with the given GUID and hands back its contents
func (c *Client) GetUserFile(id uuid.UUID) (bts []byte, err error) {
	bb := bytes.NewBuffer(nil)
	var resp *http.Response
	if resp, err = c.methodRequestURL(http.MethodGet, userFilesIdUrl(id), ``, nil); err != nil {
		return
	}
	if _, err = io.CopyN(bb, resp.Body, maxFileSize); err != nil && err != io.EOF {
		resp.Body.Close()
		return
	}
	if err = resp.Body.Close(); err == nil {
		bts = bb.Bytes()
	}
	return
}

// uploadUserFile does the dirty work of firing off a file upload
func (c *Client) uploadUserFile(method, url string, fin *os.File, meta types.UserFileDetails) (guid uuid.UUID, err error) {
	var resp *http.Response
	var fi os.FileInfo
	if fi, err = fin.Stat(); err != nil {
		return
	} else if fi.Size() > maxFileSize {
		err = ErrInvalidUserFileSize
		return
	}
	// generate the meta field
	var metaString []byte
	if metaString, err = json.Marshal(meta); err != nil {
		return
	}
	fields := map[string]string{
		userFileMetaField: string(metaString),
	}
	var ufr types.UserFileDetails
	if resp, err = c.uploadMultipartFileMethod(method, url, userFileField, `file`, fin, fields); err != nil {
		return
	} else if resp.StatusCode != 200 {
		err = fmt.Errorf("Invalid response code %d", resp.StatusCode)
	} else if err = json.NewDecoder(resp.Body).Decode(&ufr); err != nil {
		return
	} else if ufr.GUID == uuid.Nil {
		err = fmt.Errorf("Invalid response GUID")
	} else {
		err = resp.Body.Close()
	}
	guid = ufr.GUID
	return
}
