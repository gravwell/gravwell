/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/netflow"
)

func TestGenDataNetflowV5(t *testing.T) {
	seedVars(128)
	ts := time.Date(2026, 7, 19, 12, 34, 56, 789000000, time.UTC)
	var lastSeq uint32
	for i := 0; i < 256; i++ {
		ts = ts.Add(time.Second)
		b := genDataNetflowV5(ts)

		var nf netflow.NFv5
		if n, err := nf.ValidateSize(b); err != nil {
			t.Fatalf("ValidateSize failed: %v", err)
		} else if n != len(b) {
			t.Fatalf("payload size mismatch: %d != %d", n, len(b))
		}
		if err := nf.Decode(b); err != nil {
			t.Fatalf("Decode failed: %v", err)
		}
		if nf.Version != 5 {
			t.Fatalf("bad version %d", nf.Version)
		}
		if nf.Count == 0 || nf.Count > 30 {
			t.Fatalf("bad record count %d", nf.Count)
		}
		if nf.Sec != uint32(ts.Unix()) || nf.Nsec != uint32(ts.Nanosecond()) {
			t.Fatalf("bad header timestamp %d.%d != %v", nf.Sec, nf.Nsec, ts)
		}
		if i > 0 && nf.Sequence <= lastSeq {
			t.Fatalf("sequence did not advance: %d <= %d", nf.Sequence, lastSeq)
		}
		lastSeq = nf.Sequence

		for j := uint16(0); j < nf.Count; j++ {
			r := nf.Recs[j]
			if len(r.Src) != 4 || len(r.Dst) != 4 || len(r.Next) != 4 {
				t.Fatalf("record %d has non-v4 addresses: %v %v %v", j, r.Src, r.Dst, r.Next)
			}
			if r.UptimeFirst > r.UptimeLast {
				t.Fatalf("record %d flow start after end: %d > %d", j, r.UptimeFirst, r.UptimeLast)
			}
			if r.UptimeLast > nf.Uptime {
				t.Fatalf("record %d flow end after export uptime: %d > %d", j, r.UptimeLast, nf.Uptime)
			}
			switch r.Protocol {
			case 1:
				if r.SrcPort != 0 {
					t.Fatalf("record %d icmp flow has src port %d", j, r.SrcPort)
				}
			case 6, 17:
				if r.SrcPort == 0 || r.DstPort == 0 {
					t.Fatalf("record %d %d flow missing ports: %d %d", j, r.Protocol, r.SrcPort, r.DstPort)
				}
			default:
				t.Fatalf("record %d unexpected protocol %d", j, r.Protocol)
			}
			if r.Pkts == 0 || r.Bytes < r.Pkts {
				t.Fatalf("record %d bad counters pkts=%d bytes=%d", j, r.Pkts, r.Bytes)
			}
		}
	}
}
