/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package decoder

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
	"github.com/gravwell/gravwell/v3/sflow/xdr"
)

var ErrUnknownAddressIPVersion = errors.New("unknown address ip version")

func decodeXDRVariableLengthOpaque(r io.Reader) (datagram.XDRVariableLengthOpaque, error) {
	var length uint32

	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}

	// Read only the actual data bytes (without padding)
	vlo := make(datagram.XDRVariableLengthOpaque, length)

	if err := binary.Read(r, binary.BigEndian, vlo); err != nil {
		return nil, err
	}

	// Discard padding bytes
	pad := xdr.CalculatePad(length)
	if pad > 0 {
		if _, err := io.CopyN(io.Discard, r, int64(pad)); err != nil {
			return nil, err
		}
	}

	return vlo, nil
}

func decodeXDRString(r io.Reader) (datagram.XDRString, error) {
	vlo, err := decodeXDRVariableLengthOpaque(r)
	if err != nil {
		return datagram.XDRString{}, err
	}

	return datagram.XDRString{XDRVariableLengthOpaque: vlo}, nil
}

func decodeAddress(r io.Reader) (datagram.Address, error) {
	var addr datagram.Address
	if err := binary.Read(r, binary.BigEndian, &addr.Type); err != nil {
		return addr, err
	}

	var ipSize int
	switch addr.Type {
	case datagram.AddressTypeIPv4:
		ipSize = 4
	case datagram.AddressTypeIPv6:
		ipSize = 16
	case datagram.AddressTypeUnknown:
		// nothing to read here
	default:
		return addr, ErrUnknownAddressIPVersion
	}

	addr.IP = make([]byte, ipSize)
	if err := binary.Read(r, binary.BigEndian, &addr.IP); err != nil {
		return addr, err
	}

	return addr, nil
}

// decodeXDRVariableLengthArray see https://datatracker.ietf.org/doc/html/rfc4506#section-4.13
// Decodes a variable-length array of uint32 values.
// Wire format: 4 bytes (count N) + N * 4 bytes (the uint32 values).
func decodeXDRVariableLengthArray(r io.Reader) (datagram.XDRVariableLengthArray, error) {
	var count uint32
	if err := binary.Read(r, binary.BigEndian, &count); err != nil {
		return nil, err
	}

	arr := make(datagram.XDRVariableLengthArray, count)
	if err := binary.Read(r, binary.BigEndian, arr); err != nil {
		return nil, err
	}

	return arr, nil
}
