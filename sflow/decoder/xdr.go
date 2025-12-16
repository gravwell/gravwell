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
	"github.com/gravwell/gravwell/v3/sflow/xdr"
)

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
