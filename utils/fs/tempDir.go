/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package fs provides utilities related to the OS file system.
package fs

// TempDir returns a consistent platform-specific temporary directory for Gravwell.
// The returned path is guaranteed to be the same across multiple runs on the same system.
//
// On Unix-based systems (Linux, macOS), this returns /opt/gravwell/run/ (or /tmp/ as fallback
// if /opt/gravwell/run doesn't exist). This path is used for non-install scripts like ingesters,
// indexers, and other runtime components.
//
// On Windows, this returns the OS's temporary directory via os.TempDir().
func TempDir() string {
	return tempDirImpl()
}
