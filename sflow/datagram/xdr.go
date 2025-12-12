/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datagram

import "net"

// SFlow datagrams follow XDR encoding. For the purposes of the decoder, this only comes into
// play for variable length data types, since all fixed types have sizes multiple of four bytes.
//
// For these types, we must deal with the padding that ensures the total length of the data is
// a multiple of 4.

// XDRVariableLengthOpaque see https://datatracker.ietf.org/doc/html/rfc4506#section-4.10
type XDRVariableLengthOpaque []byte

// Pad refers to "r" in the specification.
func (vsa XDRVariableLengthOpaque) Pad() int {
	return len(vsa) % 4
}

// Len length of the data, without the padding. Refers to "n" in the specification.
func (vsa XDRVariableLengthOpaque) Len() int {
	return len(vsa) - vsa.Pad()
}

// FullLen length of the data, with the padding. Refers to "n + r" in the specification.
func (vsa XDRVariableLengthOpaque) FullLen() int {
	return len(vsa)
}

// XDRString see https://datatracker.ietf.org/doc/html/rfc4506#section-4.11
type XDRString struct{ XDRVariableLengthOpaque }

func (s XDRString) String() string {
	return string(s.XDRVariableLengthOpaque[:s.Len()])
}

// XDRMACAddress using sflow tooling implementation as reference. See:
// - https://github.com/sflow/host-sflow/blob/master/src/sflow/sflow.h , `_SFLMacAddress`
// - https://github.com/sflow/sflowtool/blob/master/src/sflowtool.c , `readCounters_adaptors`
type XDRMACAddress [8]byte

func (xma XDRMACAddress) MAC() net.HardwareAddr {
	if xma[7] == 0 && xma[6] == 0 {
		return net.HardwareAddr(xma[:6])
	}

	return net.HardwareAddr(xma[:])
}
