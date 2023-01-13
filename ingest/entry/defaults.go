/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package entry

const (
	DefaultTagId  EntryTag = 0
	GravwellTagId EntryTag = 0xFFFF

	MaxDataSize   uint32 = 0x3FFFFFFF
	MaxSliceCount uint32 = 0x3FFFFFFF

	flagMask uint32 = 0x3FFFFFFF

	flagIPv4 = 0x2 //bit 2
	flagEVs  = 0x1 //bit 1
)

var (
	// park these as variables so they can be overriden at build time
	DefaultTagName  string = `default`
	GravwellTagName string = `gravwell`
)
