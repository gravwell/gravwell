/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datagram

import (
	"net"

	"github.com/gravwell/gravwell/v3/sflow/xdr"
)

// IPv4 see https://sflow.org/sflow_version_5.txt, pag 24 `typedef opaque ip_v4[4]`
type IPv4 [4]byte

// IPv6 see https://sflow.org/sflow_version_5.txt, pag 24 `typedef opaque ip_v6[16]`
type IPv6 [16]byte

// AddressType see https://sflow.org/sflow_version_5.txt, pag 24 `enum address_type`
type AddressType uint32

const (
	AddressTypeUnknown AddressType = 0 // no data (0 bytes)
	AddressTypeIPv4    AddressType = 1 // 4 bytes
	AddressTypeIPv6    AddressType = 2 // 16 bytes
)

// Address see https://sflow.org/sflow_version_5.txt, pag 24 `union address (address_type type)`
type Address struct {
	Type AddressType
	IP   net.IP
}

// SFlow datagrams follow XDR encoding. For the purposes of the decoder, this only comes into
// play for variable length data types, since all fixed types have sizes multiple of four bytes.
//
// For these types, we must deal with the padding that ensures the total length of the data is
// a multiple of 4.

// XDRVariableLengthOpaque see https://datatracker.ietf.org/doc/html/rfc4506#section-4.10
// This type stores only the actual data bytes, without XDR padding.
type XDRVariableLengthOpaque []byte

// Pad returns the number of padding bytes that would be needed for XDR encoding.
// Refers to "r" in the specification.
func (vsa XDRVariableLengthOpaque) Pad() int {
	return int(xdr.CalculatePad(uint32(len(vsa))))
}

// Len returns the length of the data. Refers to "n" in the specification.
func (vsa XDRVariableLengthOpaque) Len() int {
	return len(vsa)
}

// FullLen returns the length of the data including padding (n + r in the specification).
// This is the size the data would occupy on the wire (excluding the 4-byte length prefix).
func (vsa XDRVariableLengthOpaque) FullLen() int {
	return len(vsa) + vsa.Pad()
}

// XDRString see https://datatracker.ietf.org/doc/html/rfc4506#section-4.11
type XDRString struct{ XDRVariableLengthOpaque }

func (s XDRString) String() string {
	return string(s.XDRVariableLengthOpaque)
}

// XDRMACAddress using sflow tooling implementation as reference. See:
//
// - https://github.com/sflow/host-sflow/blob/master/src/sflow/sflow.h , `_SFLMacAddress`
//
// - https://github.com/sflow/sflowtool/blob/master/src/sflowtool.c , `readCounters_adaptors`
//
// Spec does mention mac type, see https://sflow.org/sflow_version_5.txt , `typedef opaque mac[6]`
//
// However we need to add 2 bytes of padding due to XDR specification, to be a multiple of 4.
type XDRMACAddress [8]byte

func (xma XDRMACAddress) MAC() net.HardwareAddr {
	return net.HardwareAddr(xma[:6])
}

// XDRVariableLengthArray see https://datatracker.ietf.org/doc/html/rfc4506#section-4.13
// Represents a variable-length array of uint32 values.
// Encoded as: 4 bytes (count N) + N * 4 bytes (the uint32 values).
type XDRVariableLengthArray []uint32

// ASPathSegment see https://sflow.org/sflow_version_5.txt, pag 38 `union as_path_type`
type ASPathSegment struct {
	Type uint32
	ASNs XDRVariableLengthArray
}
