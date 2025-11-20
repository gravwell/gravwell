/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package datagram holds structure of the decoded datagrams
package datagram

import "net"

type Datagram struct {
	Version        uint32
	IPVersion      uint32
	AgentIP        net.IP
	SubAgentID     uint32
	SequenceNumber uint32
	Uptime         uint32
	SamplesCount   uint32
	Samples        []Sample
}
