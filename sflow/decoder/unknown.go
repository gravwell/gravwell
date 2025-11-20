/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package decoder

import (
	"io"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
)

func decodeUnknownSample(r io.Reader, format, length uint32) (*datagram.UnknownSample, error) {
	rest := make([]byte, length)
	n, err := r.Read(rest)
	if err != nil {
		return nil, err
	}

	if n != int(length) {
		return nil, ErrSampleMalformedOrIncomplete
	}

	res := datagram.UnknownSample(rest)

	return &res, nil
}
