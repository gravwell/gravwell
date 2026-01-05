/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
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

var (
	ErrUnknownAddressIPVersion = errors.New("unknown address ip version")
	ErrSizeTooLarge            = errors.New("decoded size is larger than remaining data")
)

// Minimum byte sizes for validation (per sFlow v5 spec and XDR RFC 4506)
const (
	BytesPerLength  = 1 // length field is already in bytes (XDR opaque/strings)
	BytesPerUint32  = 4 // uint32 array elements
	MinBytesPerItem = 8 // structured items (samples, records, adapters, etc)
)

func decodeXDRVariableLengthOpaque(r *io.LimitedReader) (datagram.XDRVariableLengthOpaque, error) {
	length, err := decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	// Read only the actual data bytes (without padding)
	vlo := make(datagram.XDRVariableLengthOpaque, length)

	if err := binary.Read(r, binary.BigEndian, vlo); err != nil {
		return nil, err
	}

	if err := xdr.SkipPadding(r, length); err != nil {
		return nil, err
	}

	return vlo, nil
}

func decodeXDRString(r *io.LimitedReader) (datagram.XDRString, error) {
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
func decodeXDRVariableLengthArray(r *io.LimitedReader) (datagram.XDRVariableLengthArray, error) {
	count, err := decodeLength(r, BytesPerUint32)
	if err != nil {
		return nil, err
	}

	arr := make(datagram.XDRVariableLengthArray, count)
	if err := binary.Read(r, binary.BigEndian, arr); err != nil {
		return nil, err
	}

	return arr, nil
}

// decodeLength reads a length-prefixed field from the provided LimitedReader.
// It validates that the decoded length doesn't exceed remaining bytes.
// Returns the parsed length/count value.
func decodeLength(r *io.LimitedReader, bytesPer uint32) (uint32, error) {
	var length uint32

	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return 0, err
	}

	// Use uint64 to prevent integer overflow when calculating total bytes
	totalBytes := uint64(length) * uint64(bytesPer)
	paddedBytes := totalBytes + uint64(xdr.CalculatePad(uint32(totalBytes%4)))

	if paddedBytes > uint64(r.N) {
		return 0, ErrSizeTooLarge
	}

	return length, nil
}
