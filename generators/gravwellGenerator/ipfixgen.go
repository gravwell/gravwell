/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/binary"
	"log"
	"math/rand"
	"time"

	"github.com/gravwell/ipfix"
)

// Generates native NetFlow v10 (IPFIX) messages, each entry is the data
// portion of a single IPFIX UDP packet.  The netflow ingester tracks
// template sets across packets and re-marshals each message with the
// templates its data records need so every entry stands on its own; we
// generate entries in that same self-contained form, with the template
// set leading the data sets in every message.
//
// Field specifiers are IANA IPFIX information elements, flow data is in
// the same spirit as the zeekconn and netflowv5 generators.

const (
	ipfixVersion      uint16 = 10
	ipfixV4TemplateID uint16 = 256
	ipfixV6TemplateID uint16 = 257
	ipfixDomainID     uint32 = 1
	ipfixMaxRecords          = 15
	ipfixMaxFlowMS           = 60 * 1000
)

// ipfixSequence is the running count of data records exported, per RFC 7011
var ipfixSequence uint32

var ipfixV4Template = ipfix.TemplateRecord{
	TemplateID: ipfixV4TemplateID,
	FieldSpecifiers: []ipfix.TemplateFieldSpecifier{
		{FieldID: 8, Length: 4},   // sourceIPv4Address
		{FieldID: 12, Length: 4},  // destinationIPv4Address
		{FieldID: 15, Length: 4},  // ipNextHopIPv4Address
		{FieldID: 7, Length: 2},   // sourceTransportPort
		{FieldID: 11, Length: 2},  // destinationTransportPort
		{FieldID: 4, Length: 1},   // protocolIdentifier
		{FieldID: 6, Length: 2},   // tcpControlBits
		{FieldID: 5, Length: 1},   // ipClassOfService
		{FieldID: 10, Length: 4},  // ingressInterface
		{FieldID: 14, Length: 4},  // egressInterface
		{FieldID: 2, Length: 8},   // packetDeltaCount
		{FieldID: 1, Length: 8},   // octetDeltaCount
		{FieldID: 152, Length: 8}, // flowStartMilliseconds
		{FieldID: 153, Length: 8}, // flowEndMilliseconds
	},
}

var ipfixV6Template = ipfix.TemplateRecord{
	TemplateID: ipfixV6TemplateID,
	FieldSpecifiers: []ipfix.TemplateFieldSpecifier{
		{FieldID: 27, Length: 16}, // sourceIPv6Address
		{FieldID: 28, Length: 16}, // destinationIPv6Address
		{FieldID: 62, Length: 16}, // ipNextHopIPv6Address
		{FieldID: 7, Length: 2},   // sourceTransportPort
		{FieldID: 11, Length: 2},  // destinationTransportPort
		{FieldID: 4, Length: 1},   // protocolIdentifier
		{FieldID: 6, Length: 2},   // tcpControlBits
		{FieldID: 5, Length: 1},   // ipClassOfService
		{FieldID: 10, Length: 4},  // ingressInterface
		{FieldID: 14, Length: 4},  // egressInterface
		{FieldID: 2, Length: 8},   // packetDeltaCount
		{FieldID: 1, Length: 8},   // octetDeltaCount
		{FieldID: 152, Length: 8}, // flowStartMilliseconds
		{FieldID: 153, Length: 8}, // flowEndMilliseconds
	},
}

func genDataIPFIX(ts time.Time) []byte {
	var msg ipfix.Message
	msg.Header.Version = ipfixVersion
	msg.Header.ExportTime = uint32(ts.Unix())
	msg.Header.SequenceNumber = ipfixSequence
	msg.Header.DomainID = ipfixDomainID
	// every message carries its templates so each entry stands alone,
	// exactly like the netflow ingester emits after template tracking
	msg.TemplateRecords = []ipfix.TemplateRecord{ipfixV4Template, ipfixV6Template}

	cnt := 1 + rand.Intn(ipfixMaxRecords)
	// group records by template so each message holds at most one v4 and
	// one v6 data set
	v6cnt := 0
	for i := 0; i < cnt; i++ {
		if rand.Intn(4) == 0 { //25% of flows are IPv6, same as ips()
			v6cnt++
		}
	}
	for i := 0; i < cnt-v6cnt; i++ {
		msg.DataRecords = append(msg.DataRecords, ipfixV4Record(ts))
	}
	for i := 0; i < v6cnt; i++ {
		msg.DataRecords = append(msg.DataRecords, ipfixV6Record(ts))
	}
	ipfixSequence += uint32(cnt)

	b, err := msg.Marshal()
	if err != nil {
		log.Fatalf("failed to marshal ipfix message: %v", err)
	}
	return b
}

func ipfixV4Record(ts time.Time) ipfix.DataRecord {
	next := []byte{0, 0, 0, 0}
	if rand.Intn(4) != 0 {
		next = serverIPs[rand.Intn(len(serverIPs))].To4()
	}
	return ipfix.DataRecord{
		TemplateID: ipfixV4TemplateID,
		Fields: append([][]byte{
			v4gen.IP().To4(),
			v4gen.IP().To4(),
			next,
		}, ipfixFlowFields(ts)...),
	}
}

func ipfixV6Record(ts time.Time) ipfix.DataRecord {
	next := make([]byte, 16)
	if rand.Intn(4) != 0 {
		next = serverIP6s[rand.Intn(len(serverIP6s))].To16()
	}
	return ipfix.DataRecord{
		TemplateID: ipfixV6TemplateID,
		Fields: append([][]byte{
			v6gen.IP().To16(),
			v6gen.IP().To16(),
			next,
		}, ipfixFlowFields(ts)...),
	}
}

// ipfixFlowFields generates the shared tail of both templates: ports,
// protocol, flags, ToS, interfaces, counters, and flow timestamps
func ipfixFlowFields(ts time.Time) [][]byte {
	var srcPort, dstPort uint16
	var flags uint16
	proto := nfv5Protos[rand.Intn(len(nfv5Protos))]
	pkts := uint64(1 + rand.Intn(10000))
	bytes := pkts * uint64(40+rand.Intn(1460))
	switch proto {
	case 1: // icmp encodes type/code in the dst port field
		dstPort = nfv5ICMPCodes[rand.Intn(len(nfv5ICMPCodes))]
		pkts = uint64(1 + rand.Intn(10))
		bytes = pkts * uint64(64+rand.Intn(64))
	case 6:
		spt, dpt := ports()
		srcPort, dstPort = uint16(spt), uint16(dpt)
		flags = uint16(nfv5TCPFlags[rand.Intn(len(nfv5TCPFlags))])
	default:
		spt, dpt := ports()
		srcPort, dstPort = uint16(spt), uint16(dpt)
	}
	var tos byte
	if rand.Intn(2) == 0 {
		tos = byte(rand.Intn(64)) << 2 // random DSCP, zero ECN
	}

	end := ts.Add(-time.Duration(rand.Intn(1000)) * time.Millisecond)
	start := end.Add(-time.Duration(rand.Intn(ipfixMaxFlowMS)) * time.Millisecond)

	return [][]byte{
		ipfixU16(srcPort),
		ipfixU16(dstPort),
		{proto},
		ipfixU16(flags),
		{tos},
		ipfixU32(uint32(1 + rand.Intn(8))),
		ipfixU32(uint32(1 + rand.Intn(8))),
		ipfixU64(pkts),
		ipfixU64(bytes),
		ipfixU64(uint64(start.UnixMilli())),
		ipfixU64(uint64(end.UnixMilli())),
	}
}

func ipfixU16(v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}

func ipfixU32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func ipfixU64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}
