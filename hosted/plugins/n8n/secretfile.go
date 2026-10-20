/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package n8n

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const maximumCredentialFileSize = 64 << 10

func readCredentialFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("Credential-File is required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("Credential-File: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("Credential-File must be a regular file and not a symbolic link")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("Credential-File: %w", err)
	}
	defer f.Close()

	openedInfo, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("Credential-File: %w", err)
	}
	if !os.SameFile(info, openedInfo) {
		return "", errors.New("Credential-File changed during validation")
	}
	if !openedInfo.Mode().IsRegular() {
		return "", errors.New("Credential-File must be a regular file and not a symbolic link")
	}
	if openedInfo.Size() < 1 || openedInfo.Size() > maximumCredentialFileSize {
		return "", fmt.Errorf("Credential-File must be between 1 byte and %d bytes", maximumCredentialFileSize)
	}
	if openedInfo.Mode().Perm()&0o077 != 0 {
		return "", errors.New("Credential-File must not be accessible by group or other users")
	}
	body, err := readCredentialContents(f, openedInfo.Size())
	if err != nil {
		return "", fmt.Errorf("Credential-File: %w", err)
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", errors.New("Credential-File is empty")
	}
	return value, nil
}

func readCredentialContents(r io.Reader, expectedSize int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maximumCredentialFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) < 1 || len(body) > maximumCredentialFileSize {
		return nil, fmt.Errorf("Credential-File must be between 1 byte and %d bytes", maximumCredentialFileSize)
	}
	if int64(len(body)) != expectedSize {
		return nil, errors.New("Credential-File changed during read")
	}
	return body, nil
}
