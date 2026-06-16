/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

// File contains metadata about the file, but not the actual bytes.
type File struct {
	CommonFields

	Size          uint64
	Hash          string
	ContentType   string // Guessed at update time if possible
	FileExtension string // The extension of the uploaded file, with the dot (ex: ".csv").
}

type FileListResponse struct {
	BaseListResponse
	Results []File `json:"results"`
}
