/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package xdr provides utilities for XDR (External Data Representation) encoding
// as used in sFlow datagrams. See https://datatracker.ietf.org/doc/html/rfc4506
package xdr

// CalculatePad returns the number of padding bytes needed to align the given
// length to a 4-byte boundary, as required by XDR encoding.
//
// Per RFC 4506, variable-length data must be padded to a multiple of 4 bytes.
// The padding bytes are zero-filled and not included in the length count.
func CalculatePad(length uint32) uint32 {
	rest := length % 4
	if rest == 0 {
		return 0
	}
	return 4 - rest
}

// PaddedLength returns the total length including padding bytes.
func PaddedLength(length uint32) uint32 {
	return length + CalculatePad(length)
}
