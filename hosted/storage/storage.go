/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package storage contains implementations of the storage interface for hosted runtimes.
package storage

import "errors"

var (
	ErrStorageNotFound = errors.New("storage not found")
)
