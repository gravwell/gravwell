/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

// File contains metadata about the file, but not the actual bytes.
// The file's contents are fetched and set separately via GetFile and PopulateFile, respectively.
type File struct {
	CommonFields

	Size uint64
	Hash string
}

type FileListResponse struct {
	BaseListResponse
	Results []File `json:"results"`
}
