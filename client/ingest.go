/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"

	"github.com/gravwell/gravwell/v3/client/types"
)

// IngestEntries takes an array of entries and uploads them to the webserver, which
// will then distribute them out to its indexers.
// Returns the number of ingested entries and any error.
func (c *Client) IngestEntries(entries []types.StringTagEntry) error {
	return c.putStaticURL(JSON_INGEST_URL, entries)
}

// IngestFile uploads the contents of a file on disk and ingests them.
//
// The 'file' argument should point at a valid file on disk containing line-delimited log entries,
// a pcap packet capture, or JSON as downloaded from Gravwell search results.
//
// 'tag' is the tag to use, and 'src' should be a string containing a valid IP address.
//
// If 'ignoreTimestamp' is set, all entries will be tagged with the current time.
//
// If 'assumeLocalTimezone' is set, any timezone information in the data will be ignored and
// timestamps will be assumed to be in the Gravwell server's local timezone.
func (c *Client) IngestFile(file, tag, src string, ignoreTimestamp, assumeLocalTimezone bool) (resp types.IngestResponse, err error) {
	fin, err := os.Open(file)
	if err != nil {
		return
	}
	defer fin.Close()
	fi, err := fin.Stat()
	if err != nil {
		return
	}
	if fi.Size() <= 0 {
		err = errors.New("file is empty")
		return
	}

	r, w := io.Pipe()

	wtr := multipart.NewWriter(w)

	go func() {
		defer w.Close()
		// set the tag
		wtr.WriteField("tag", tag)
		wtr.WriteField("source", src)
		// set options if given
		if ignoreTimestamp {
			wtr.WriteField("noparsetimestamp", "true")
		}
		if assumeLocalTimezone {
			wtr.WriteField("assumelocaltimezone", "true")
		}
		// copy in the file
		mp, err := wtr.CreateFormFile(`file`, `line-delimited-file`)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create multipart form file: %v\n", err)
			return
		}
		if _, err := io.Copy(mp, fin); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to copy data into multipart file: %v\n", err)
			return
		}
		// now finalize
		if err := wtr.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to finalize multipart request: %v\n", err)
			return
		}
	}()

	// and ship
	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, LINES_INGEST_URL)
	req, err := http.NewRequest(http.MethodPost, uri, r)
	if err != nil {
		return
	}
	req.Header.Set(`Content-Type`, wtr.FormDataContentType())
	okResps := []int{http.StatusOK, http.StatusMultiStatus}
	if err = c.staticRequest(req, &resp, okResps); err != nil {
		if err != io.EOF {
			return
		}
		//if the error is EOF, it means that there was no response
		//which means total success!
	}
	return
}
