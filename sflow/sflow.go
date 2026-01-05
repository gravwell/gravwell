/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package sflow implements a low level high speed sflowV5 decoder
package sflow

import (
	"io"

	"github.com/gravwell/gravwell/v3/sflow/decoder"
)

// NewDecoder returns a new sFlow datagram decoder. The reader provided should have a sflow datagram byte stream.
func NewDecoder(r io.Reader) decoder.DatagramDecoder {
	return decoder.NewDatagramDecoder(r)
}
