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
	nfv5Reset()
	ts := time.Date(2026, 7, 19, 12, 34, 56, 789000000, time.UTC)
	var lastSeq uint32
	for i := range 256 {
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

// TestNetflowV5UptimeSpan walks a span far longer than the ~49.7 days a uint32
// millisecond uptime can hold, and checks that we reboot the exporter rather
// than letting the field truncate.  v5 shares netflowUptime with v9.
func TestNetflowV5UptimeSpan(t *testing.T) {
	seedVars(128)
	nfv5Reset()
	ts := time.Now().Add(-120 * 24 * time.Hour)
	var prevUptime, prevSeq uint32
	var reboots int
	for i := 0; i < 4096; i++ {
		var nf netflow.NFv5
		if err := nf.Decode(genDataNetflowV5(ts)); err != nil {
			t.Fatalf("iteration %d: decode failed: %v", i, err)
		}
		if i > 0 && nf.Uptime < prevUptime {
			// only an explicit reboot may move uptime backwards, and it
			// has to restart the export sequence with it
			if nf.Sequence != 0 {
				t.Fatalf("iteration %d: uptime fell from %d to %d without a sequence reset (seq %d)",
					i, prevUptime, nf.Uptime, nf.Sequence)
			}
			reboots++
		} else if i > 0 && nf.Sequence < prevSeq {
			t.Fatalf("iteration %d: sequence fell from %d to %d", i, prevSeq, nf.Sequence)
		}
		prevUptime, prevSeq = nf.Uptime, nf.Sequence

		for j := uint16(0); j < nf.Count; j++ {
			r := nf.Recs[j]
			if r.UptimeFirst > r.UptimeLast || r.UptimeLast > nf.Uptime {
				t.Fatalf("iteration %d record %d: %d/%d against uptime %d",
					i, j, r.UptimeFirst, r.UptimeLast, nf.Uptime)
			}
			// uptime is always well past a flow's worth of milliseconds
			// here, so nothing should be riding the underflow guards
			if nf.Uptime > 2*nfv5MaxFlowMS && (r.UptimeFirst == 0 || r.UptimeLast == 0) {
				t.Fatalf("iteration %d record %d: switch times collapsed to zero at uptime %d",
					i, j, nf.Uptime)
			}
		}
		ts = ts.Add(time.Hour)
	}
	// 4096 hours is over 170 days, the exporter has to have rebooted
	if reboots == 0 {
		t.Fatal("no reboot across a 170 day span, uptime must have wrapped silently")
	}
	t.Logf("modeled %d exporter reboots across the span", reboots)
}

// nfv5Reset clears the exporter state so a test does not inherit the boot time
// and sequence counter left behind by whatever ran before it
func nfv5Reset() {
	nfv5Boot = time.Time{}
	nfv5Sequence = 0
}
