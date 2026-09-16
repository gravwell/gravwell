/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"log"
	"math/rand"
	"time"

	"github.com/gravwell/ipfix"
)

// Generates native NetFlow v9 messages, each entry is the data portion of a
// single NetFlow v9 UDP packet.  The netflow ingester handles v9 and IPFIX
// with the same session machinery: it tracks template sets across packets and
// re-marshals each message with the templates its data records need so every
// entry stands on its own.  We generate entries in that same self-contained
// form, with the template FlowSet leading the data FlowSets in every message.
//
// Field specifiers are RFC 3954 field types, which are not the same numbering
// space as the IPFIX information elements used by the ipfix generator, so v9
// carries its own templates.  Flow data is in the same spirit as the zeekconn,
// netflowv5, and ipfix generators.

const (
	nfv9Version      uint16 = 9
	nfv9V4TemplateID uint16 = 256
	nfv9V6TemplateID uint16 = 257
	nfv9SourceID     uint32 = 1
	nfv9MaxRecords          = 15
	nfv9MaxFlowMS           = 60 * 1000
	// keep uptime well below the uint32 millisecond wrap (~49.7 days)
	nfv9MaxBootBehind = 40 * 24 * time.Hour
)

var (
	// nfv9Sequence counts exported packets, per RFC 3954.  Note that this
	// differs from IPFIX, where the sequence number counts data records.
	nfv9Sequence uint32

	// nfv9Boot is the synthetic boot time of the exporter, FIRST_SWITCHED
	// and LAST_SWITCHED are milliseconds since it
	nfv9Boot time.Time
)

var nfv9V4Template = ipfix.TemplateRecord{
	TemplateID: nfv9V4TemplateID,
	FieldSpecifiers: append([]ipfix.TemplateFieldSpecifier{
		{FieldID: 8, Length: 4},  // IPV4_SRC_ADDR
		{FieldID: 12, Length: 4}, // IPV4_DST_ADDR
		{FieldID: 15, Length: 4}, // IPV4_NEXT_HOP
		{FieldID: 9, Length: 1},  // SRC_MASK
		{FieldID: 13, Length: 1}, // DST_MASK
	}, nfv9CommonFieldSpecifiers...),
}

var nfv9V6Template = ipfix.TemplateRecord{
	TemplateID: nfv9V6TemplateID,
	FieldSpecifiers: append([]ipfix.TemplateFieldSpecifier{
		{FieldID: 27, Length: 16}, // IPV6_SRC_ADDR
		{FieldID: 28, Length: 16}, // IPV6_DST_ADDR
		{FieldID: 62, Length: 16}, // IPV6_NEXT_HOP
		{FieldID: 29, Length: 1},  // IPV6_SRC_MASK
		{FieldID: 30, Length: 1},  // IPV6_DST_MASK
	}, nfv9CommonFieldSpecifiers...),
}

// nfv9CommonFieldSpecifiers is the tail shared by both templates, everything
// that does not depend on the address family
var nfv9CommonFieldSpecifiers = []ipfix.TemplateFieldSpecifier{
	{FieldID: 7, Length: 2},  // L4_SRC_PORT
	{FieldID: 11, Length: 2}, // L4_DST_PORT
	{FieldID: 4, Length: 1},  // PROTOCOL
	{FieldID: 6, Length: 1},  // TCP_FLAGS
	{FieldID: 5, Length: 1},  // TOS
	{FieldID: 10, Length: 2}, // INPUT_SNMP
	{FieldID: 14, Length: 2}, // OUTPUT_SNMP
	{FieldID: 16, Length: 2}, // SRC_AS
	{FieldID: 17, Length: 2}, // DST_AS
	{FieldID: 2, Length: 4},  // IN_PKTS
	{FieldID: 1, Length: 4},  // IN_BYTES
	{FieldID: 22, Length: 4}, // FIRST_SWITCHED
	{FieldID: 21, Length: 4}, // LAST_SWITCHED
}

func genDataNetflowV9(ts time.Time) []byte {
	if nfv9Boot.IsZero() || ts.Before(nfv9Boot) {
		nfv9Boot = ts.Add(-time.Duration(rand.Int63n(int64(nfv9MaxBootBehind))))
	}
	uptime := uint32(ts.Sub(nfv9Boot).Milliseconds())

	var msg ipfix.Message
	msg.Header.Version = nfv9Version
	msg.Header.SysUptime = uptime
	msg.Header.ExportTime = uint32(ts.Unix())
	msg.Header.SequenceNumber = nfv9Sequence
	msg.Header.DomainID = nfv9SourceID // "source ID" in netflow v9
	// every message carries its templates so each entry stands alone,
	// exactly like the netflow ingester emits after template tracking
	msg.TemplateRecords = []ipfix.TemplateRecord{nfv9V4Template, nfv9V6Template}
	nfv9Sequence++ // v9 sequence counts packets, not records

	cnt := 1 + rand.Intn(nfv9MaxRecords)
	// group records by template so each message holds at most one v4 and
	// one v6 data FlowSet
	v6cnt := 0
	for range cnt {
		if rand.Intn(4) == 0 { //25% of flows are IPv6, same as ips()
			v6cnt++
		}
	}
	for i := 0; i < cnt-v6cnt; i++ {
		msg.DataRecords = append(msg.DataRecords, nfv9V4Record(uptime))
	}
	for i := 0; i < v6cnt; i++ {
		msg.DataRecords = append(msg.DataRecords, nfv9V6Record(uptime))
	}

	b, err := msg.Marshal()
	if err != nil {
		log.Fatalf("failed to marshal netflow v9 message: %v", err)
	}
	return b
}

func nfv9V4Record(uptime uint32) ipfix.DataRecord {
	next := []byte{0, 0, 0, 0}
	if rand.Intn(4) != 0 {
		next = serverIPs[rand.Intn(len(serverIPs))].To4()
	}
	return ipfix.DataRecord{
		TemplateID: nfv9V4TemplateID,
		Fields: append([][]byte{
			v4gen.IP().To4(),
			v4gen.IP().To4(),
			next,
			{byte(8 + rand.Intn(25))},
			{byte(8 + rand.Intn(25))},
		}, nfv9FlowFields(uptime)...),
	}
}

func nfv9V6Record(uptime uint32) ipfix.DataRecord {
	next := make([]byte, 16)
	if rand.Intn(4) != 0 {
		next = serverIP6s[rand.Intn(len(serverIP6s))].To16()
	}
	return ipfix.DataRecord{
		TemplateID: nfv9V6TemplateID,
		Fields: append([][]byte{
			v6gen.IP().To16(),
			v6gen.IP().To16(),
			next,
			{byte(32 + rand.Intn(33))},
			{byte(32 + rand.Intn(33))},
		}, nfv9FlowFields(uptime)...),
	}
}

// nfv9FlowFields generates the shared tail of both templates: ports, protocol,
// flags, ToS, interfaces, AS numbers, counters, and flow timestamps
func nfv9FlowFields(uptime uint32) [][]byte {
	var srcPort, dstPort uint16
	var flags byte
	proto := nfv5Protos[rand.Intn(len(nfv5Protos))]
	pkts := uint32(1 + rand.Intn(10000))
	bytes := pkts * uint32(40+rand.Intn(1460))
	switch proto {
	case 1: // icmp encodes type/code in the dst port field
		dstPort = nfv5ICMPCodes[rand.Intn(len(nfv5ICMPCodes))]
		pkts = uint32(1 + rand.Intn(10))
		bytes = pkts * uint32(64+rand.Intn(64))
	case 6:
		spt, dpt := ports()
		srcPort, dstPort = uint16(spt), uint16(dpt)
		flags = nfv5TCPFlags[rand.Intn(len(nfv5TCPFlags))]
	default:
		spt, dpt := ports()
		srcPort, dstPort = uint16(spt), uint16(dpt)
	}
	var tos byte
	if rand.Intn(2) == 0 {
		tos = byte(rand.Intn(64)) << 2 // random DSCP, zero ECN
	}

	// v9 switch times are milliseconds since the exporter booted, the flow
	// started and ended before the uptime in the message header
	last := uptime - uint32(rand.Intn(1000))
	if last > uptime {
		last = 0 // underflow guard for freshly booted exporters
	}
	first := last - uint32(rand.Intn(nfv9MaxFlowMS))
	if first > last {
		first = 0
	}

	return [][]byte{
		ipfixU16(srcPort),
		ipfixU16(dstPort),
		{proto},
		{flags},
		{tos},
		ipfixU16(uint16(1 + rand.Intn(8))),
		ipfixU16(uint16(1 + rand.Intn(8))),
		ipfixU16(uint16(rand.Intn(0xffff))),
		ipfixU16(uint16(rand.Intn(0xffff))),
		ipfixU32(pkts),
		ipfixU32(bytes),
		ipfixU32(first),
		ipfixU32(last),
	}
}
