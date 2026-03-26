/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

// Userfile contains metadata about the file, but not the actual bytes.
type File struct {
	CommonFields

	Size uint64
	Hash string
}

type FileListResponse struct {
	BaseListResponse
	Results []File `json:"results"`
}

func CleanupFiles() error
func CreateFile(t File) (result File, err error)
func (File) DeleteFile(id string) error
func (File) GetFile(id string) (File, error)
func (File) GetFileEx(id string, opts *QueryOptions) (File, error)
func (File) ListAllFiles(opts *QueryOptions) (ret FileListResponse, err error)
func (File) ListFiles(opts *QueryOptions) (ret FileListResponse, err error)
func (File) PurgeFile(id string) error
func (File) UpdateFile(t File) (result File, err error)
