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
	"math"
	"math/rand"
	"time"

	"github.com/gravwell/gravwell/v4/netflow"
)

// Generates native NetFlow v5 flow sets, each entry is the data portion of
// a single NetFlow v5 UDP packet (header + records) as encoded by the
// gravwell netflow package.  Flow data is in the same spirit as the
// zeekconn generator: random v4 talkers, ports, and counters.

const (
	nfv5MaxRecords = 30 // hard cap in the NetFlow v5 spec
	nfv5MaxFlowMS  = 60 * 1000

	// netflowMaxUptimeMS is the ceiling of the uint32 millisecond uptime
	// fields shared by NetFlow v5 and v9, about 49.7 days
	netflowMaxUptimeMS = int64(math.MaxUint32)

	// netflowMaxBootBehind bounds how much uptime an exporter starts with,
	// leaving the rest of the uint32 range for the run to advance through
	netflowMaxBootBehind = 7 * 24 * time.Hour
)

var (
	nfv5Sequence uint32
	nfv5Boot     time.Time

	// IANA protocol numbers for the same protos the zeek conn generator uses
	nfv5Protos = []byte{
		1,  // icmp
		6,  // tcp
		6,  // tcp (weighted)
		6,  // tcp (weighted)
		17, // udp
		17, // udp (weighted)
	}

	// plausible cumulative TCP flag sets: syn scan, complete handshakes,
	// resets, and full sessions
	nfv5TCPFlags = []byte{
		0x02,       // SYN
		0x12,       // SYN|ACK
		0x14,       // RST|ACK
		0x19,       // FIN|PSH|ACK
		0x1b, 0x1b, // SYN|FIN|PSH|ACK - complete session (weighted)
		0x1f, // full teardown with RST
	}

	// common ICMP type/code pairs as encoded in the NetFlow v5 dst port
	// field: echo reply, dest unreachable (host), echo request, time exceeded
	nfv5ICMPCodes = []uint16{0x0000, 0x0301, 0x0800, 0x0800, 0x0b00}
)

// netflowUptime returns the exporter uptime in milliseconds at ts, rebooting
// the exporter whenever the uptime field cannot represent the answer.  NetFlow
// v5 and v9 both carry uptime and flow switch times as uint32 milliseconds, so
// an exporter can only stay up for about 49.7 days before the field wraps.
// Generator runs can easily span longer than that (-duration takes day and week
// suffixes), and letting the conversion truncate would silently emit flow times
// ~49 days in the past for the tail of the run.  Instead we model what a real
// exporter does when it runs out of uptime: reboot.  Uptime restarts near zero
// and the export sequence counter restarts with it.  The same happens when ts
// walks backwards behind the boot time, which chaos mode timestamps can do.
func netflowUptime(ts time.Time, boot *time.Time, seq *uint32) uint32 {
	if !boot.IsZero() {
		if ms := ts.Sub(*boot).Milliseconds(); ms >= 0 && ms <= netflowMaxUptimeMS {
			return uint32(ms)
		}
		*seq = 0 // a reboot restarts the export sequence
	}
	*boot = ts.Add(-time.Duration(rand.Int63n(int64(netflowMaxBootBehind))))
	return uint32(ts.Sub(*boot).Milliseconds())
}

func genDataNetflowV5(ts time.Time) []byte {
	uptime := netflowUptime(ts, &nfv5Boot, &nfv5Sequence)

	var nf netflow.NFv5
	nf.Version = 5
	nf.Count = uint16(1 + rand.Intn(nfv5MaxRecords))
	nf.Uptime = uptime
	nf.Sec = uint32(ts.Unix())
	nf.Nsec = uint32(ts.Nanosecond())
	nf.Sequence = nfv5Sequence
	nf.EngineID = byte(rand.Intn(4))
	nfv5Sequence += uint32(nf.Count)

	for i := uint16(0); i < nf.Count; i++ {
		nfv5FillRecord(&nf.Recs[i], uptime)
	}
	b, err := nf.Encode()
	if err != nil {
		log.Fatalf("failed to encode netflow v5 flow: %v", err)
	}
	return b
}

func nfv5FillRecord(r *netflow.NFv5Record, uptime uint32) {
	r.Src = v4gen.IP().To4()
	r.Dst = v4gen.IP().To4()
	if rand.Intn(4) == 0 {
		// some flows are directly connected, no next hop
		r.Next = []byte{0, 0, 0, 0}
	} else {
		r.Next = serverIPs[rand.Intn(len(serverIPs))].To4()
	}
	r.Input = uint16(1 + rand.Intn(8))
	r.Output = uint16(1 + rand.Intn(8))

	r.Pkts = uint32(1 + rand.Intn(10000))
	r.Bytes = r.Pkts * uint32(40+rand.Intn(1460))

	// flow started and ended before the export uptime
	last := uptime - uint32(rand.Intn(1000))
	if last > uptime {
		last = 0 // underflow guard for freshly booted exporters
	}
	first := last - uint32(rand.Intn(nfv5MaxFlowMS))
	if first > last {
		first = 0
	}
	r.UptimeFirst = first
	r.UptimeLast = last

	r.Protocol = nfv5Protos[rand.Intn(len(nfv5Protos))]
	switch r.Protocol {
	case 1: // icmp encodes type/code in the dst port field
		r.SrcPort = 0
		r.DstPort = nfv5ICMPCodes[rand.Intn(len(nfv5ICMPCodes))]
		r.Pkts = uint32(1 + rand.Intn(10))
		r.Bytes = r.Pkts * uint32(64+rand.Intn(64))
	case 6:
		spt, dpt := ports()
		r.SrcPort, r.DstPort = uint16(spt), uint16(dpt)
		r.Flags = nfv5TCPFlags[rand.Intn(len(nfv5TCPFlags))]
	default:
		spt, dpt := ports()
		r.SrcPort, r.DstPort = uint16(spt), uint16(dpt)
	}
	if rand.Intn(2) == 0 {
		r.ToS = 0
	} else {
		r.ToS = byte(rand.Intn(64)) << 2 // random DSCP, zero ECN
	}
	r.SrcAs = uint16(rand.Intn(0xffff))
	r.DstAs = uint16(rand.Intn(0xffff))
	r.SrcMask = byte(8 + rand.Intn(25))
	r.DstMask = byte(8 + rand.Intn(25))
}
