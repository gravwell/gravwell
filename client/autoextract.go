package client

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/gravwell/gravwell/v4/client/types"
)

// ListExtractions returns the list of autoextraction definitions available
// to the current user.
func (c *Client) ListExtractions(opts types.QueryOptions) (ret types.AXListResponse, err error) {
	err = c.postStaticURL(EXTRACTORS_LIST_URL, opts, &ret)
	return
}

// ListAllExtractions returns the list of autoextraction definitions available
// to the current user, setting admin mode to true -- admin users will receive ALL definitions.
func (c *Client) ListAllExtractions(opts types.QueryOptions) (ret types.AXListResponse, err error) {
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	return c.post[types.QueryOptions, types.AXListResponse](EXTRACTORS_LIST_URL, &opts)
}

// GetExtraction returns a particular extraction by UUID
func (c *Client) GetExtraction(id string) (d types.AX, err error) {
	err = c.getStaticURL(extractionIdUrl(id), &d)
	return
}

// FindExtraction returns the most appropriate extraction for a given tag
func (c *Client) FindExtraction(tag string) (d types.AX, err error) {
	err = c.getStaticURL(extractionFindUrl(tag), &d)
	return
}

// DeleteExtraction deletes the specified autoextraction.
func (c *Client) DeleteExtraction(id string) (wrs []types.WarnResp, err error) {
	if err = c.deleteStaticURL(extractionIdUrl(id), nil); err == io.EOF {
		err = nil
	}
	return
}

// PurgeExtraction deletes the specified autoextraction.
func (c *Client) PurgeExtraction(id string) (wrs []types.WarnResp, err error) {
	if err = c.deleteStaticURL(extractionIdUrl(id), nil, ezParam("purge", "true")); err == io.EOF {
		err = nil
	}
	return
}

// ValidateExtraction validates an autoextractor definition.
func (c *Client) ValidateExtraction(d types.AX) (wrs []types.WarnResp, err error) {
	if err = c.postStaticURL(extractionsTestUrl(), d, nil); err == io.EOF {
		err = nil
	}
	return
}

// CreateExtraction installs an autoextractor definition, returning the UUID of the new
// extraction or an error if it is invalid.
func (c *Client) CreateExtraction(d types.AX) (result types.AX, wrs []types.WarnResp, err error) {
	if err = c.postStaticURL(extractionsUrl(), d, &result); err == io.EOF {
		err = nil
	}
	return
}

// UpdateExtraction modifies an existing autoextractor and returns the complete, updated struct.
func (c *Client) UpdateExtraction(ID string, p types.AXPatch) (updated types.AX, err error) {
	if ID == "" {
		return types.AX{}, ErrEmptyID
	}
	return c.patch[types.AXPatch, types.AX](extractionIdUrl(ID), p)
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
	defer drainResponse(resp)
	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("Bad Status %s(%d): %v", resp.Status, resp.StatusCode, getBodyErr(resp.Body))
		return
	}
	return
}
