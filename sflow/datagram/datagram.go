/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package datagram holds structure of the decoded sflow datagrams
package datagram

import "net"

// Datagram see https://sflow.org/sflow_version_5.txt, pag 32, `sample_datagram_v5`
type Datagram struct {
	Version   uint32
	IPVersion uint32
	// Can be 4 bytes for IP V4, 16 bytes for IP V6
	AgentIP        net.IP
	SubAgentID     uint32
	SequenceNumber uint32
	Uptime         uint32
	SamplesCount   uint32
	Samples        []Sample
}
