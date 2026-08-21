/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import "encoding/json"

// File contains metadata about the file, but not the actual bytes.
type File struct {
	CommonFields

	Size          uint64
	Hash          string
	ContentType   string // Guessed at update time if possible
	FileExtension string // The extension of the uploaded file, with the dot (ex: ".csv").
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (f File) MarshalJSON() ([]byte, error) {
	type dummyFile File
	f.CommonFields = f.CommonFields.MakeNilSlices()
	return json.Marshal(dummyFile(f))
}

type FileListResponse struct {
	BaseListResponse
	Results []File
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (f FileListResponse) MarshalJSON() ([]byte, error) {
	type dummyFileListResponse FileListResponse
	f.Results = nonNilSlice(f.Results)
	f.BaseListResponse = f.BaseListResponse.MakeNilSlices()
	return json.Marshal(dummyFileListResponse(f))
}
