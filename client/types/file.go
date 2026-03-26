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
func DeleteFile(id string) error
func GetFile(id string) (File, error)
func GetFileEx(id string, opts *QueryOptions) (File, error)
func ListAllFiles(opts *QueryOptions) (ret FileListResponse, err error)
func ListFiles(opts *QueryOptions) (ret FileListResponse, err error)
func PurgeFile(id string) error
func UpdateFile(t File) (result File, err error)
