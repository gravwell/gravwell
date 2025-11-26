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
	"io"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
)

func calculatePad(l uint32) uint32 {
	return 4 - (l % 4)
}

func decodeXDRVariableLengthOpaque(r io.Reader) (datagram.XDRVariableLengthOpaque, error) {
	var length uint32

	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}

	vlo := make(datagram.XDRVariableLengthOpaque, length+calculatePad(length))

	if err := binary.Read(r, binary.BigEndian, vlo); err != nil {
		return nil, err
	}

	return vlo, nil
}

// TODO  Consider adding a "maxSize" parameter, but only if every string I am finding has a max size else make a "decodeXDRStringLimit" func
func decodeXDRString(r io.Reader) (datagram.XDRString, error) {
	vlo, err := decodeXDRVariableLengthOpaque(r)
	if err != nil {
		return datagram.XDRString{}, err
	}

	return datagram.XDRString{XDRVariableLengthOpaque: vlo}, nil
}

func decodeXDRMACAddress(r io.Reader) (datagram.XDRMACAddress, error) {
	xvlo, err := decodeXDRVariableLengthOpaque(r)
	if err != nil {
		return datagram.XDRMACAddress{}, err
	}

	return datagram.XDRMACAddress{
		XDRVariableLengthOpaque: xvlo,
	}, nil
}
