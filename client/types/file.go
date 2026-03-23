/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

// Userfile contains metadata about the file, but not the actual bytes.
type UserFile struct {
	CommonFields

	Size uint64
	Hash string
}
