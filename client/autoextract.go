package client

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
)

// ExtractionSupportedEngines returns a list of valid engines for use in
// autoextraction definitions.
func (c *Client) ExtractionSupportedEngines() (v []string, err error) {
	return c.get[[]string](extractionEnginesUrl())
}

// ListExtractions returns the list of autoextraction definitions available
// to the current user.
func (c *Client) ListExtractions(opts types.QueryOptions) (ret types.AXListResponse, err error) {
	return c.post[types.QueryOptions, types.AXListResponse](EXTRACTORS_LIST_URL, &opts)
}

// ListAllExtractions returns the list of autoextraction definitions available
// to the current user, setting admin mode to true -- admin users will receive ALL definitions.
func (c *Client) ListAllExtractions(opts types.QueryOptions) (ret types.AXListResponse, err error) {
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	return c.post[types.QueryOptions, types.AXListResponse](EXTRACTORS_LIST_URL, &opts)
}

// GetExtraction returns a particular extraction by UUID
func (c *Client) GetExtraction(id string) (d types.AX, err error) {
	return c.get[types.AX](extractionIdUrl(id))
}

// FindExtraction returns the most appropriate extraction for a given tag
func (c *Client) FindExtraction(tag string) (d types.AX, err error) {
	return c.get[types.AX](extractionFindUrl(tag))
}

// DeleteExtraction deletes the specified autoextraction.
//
// NOTE: Extractions are always hard-deleted.
func (c *Client) DeleteExtraction(id string) (err error) {
	return c.delete(extractionIdUrl(id), false)
}

type AXValidateResponse struct {
	TagExists bool // does this tag already exist?
	Error     string
}

// ValidateExtraction validates an autoextractor definition.
func (c *Client) ValidateExtraction(d types.AX) (tagExists bool, err error) {
	axvr, err := c.post[types.AX, AXValidateResponse](extractionsTestUrl(), &d)
	if err != nil {
		return false, err
	} else if strings.TrimSpace(axvr.Error) != "" {
		// this should never actually happen as it should be caught by c.post, but just in case
		return false, errors.New(axvr.Error)
	}
	return axvr.TagExists, nil
}

// CreateExtraction installs an autoextractor definition, returning the newly-created autoextractor.
func (c *Client) CreateExtraction(d types.AX) (result types.AX, err error) {
	return c.post[types.AX, types.AX](extractionsUrl(), &d)
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
