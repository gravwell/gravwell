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
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/gravwell/ipfix"
)

// nfv9Reset clears the exporter state so a test does not inherit the boot time
// and sequence counter left behind by whatever ran before it
func nfv9Reset() {
	nfv9Boot = time.Time{}
	nfv9Sequence = 0
}

// nfv9RecordLen is the wire size of one data record for the given template
func nfv9RecordLen(tr ipfix.TemplateRecord) int {
	var n int
	for _, fs := range tr.FieldSpecifiers {
		n += int(fs.Length)
	}
	return n
}

func TestGenDataNetflowV9(t *testing.T) {
	seedVars(128)
	nfv9Reset()
	ts := time.Date(2026, 7, 19, 12, 34, 56, 0, time.UTC)
	var expectedSeq uint32
	first := true
	for range 256 {
		ts = ts.Add(time.Second)
		b := genDataNetflowV9(ts)

		// each entry must stand on its own: a brand new session with no
		// prior template knowledge has to fully decode it
		s := ipfix.NewSession()
		msg, err := s.ParseBuffer(b)
		if err != nil {
			t.Fatalf("fresh session failed to parse message: %v", err)
		}
		if msg.Header.Version != 9 {
			t.Fatalf("bad version %d", msg.Header.Version)
		}
		if msg.Header.ExportTime != uint32(ts.Unix()) {
			t.Fatalf("bad export time %d != %d", msg.Header.ExportTime, ts.Unix())
		}
		if msg.Header.DomainID != nfv9SourceID {
			t.Fatalf("bad source id %d", msg.Header.DomainID)
		}
		if len(msg.TemplateRecords) != 2 {
			t.Fatalf("expected 2 template records, got %d", len(msg.TemplateRecords))
		}
		// both templates must come back exactly as declared, not merely
		// with the right number of fields (v4 and v6 have the same count)
		for _, tr := range msg.TemplateRecords {
			var want []ipfix.TemplateFieldSpecifier
			switch tr.TemplateID {
			case nfv9V4TemplateID:
				want = nfv9V4Template.FieldSpecifiers
			case nfv9V6TemplateID:
				want = nfv9V6Template.FieldSpecifiers
			default:
				t.Fatalf("unexpected template id %d", tr.TemplateID)
			}
			if !reflect.DeepEqual(tr.FieldSpecifiers, want) {
				t.Fatalf("template %d round tripped as %+v, want %+v",
					tr.TemplateID, tr.FieldSpecifiers, want)
			}
		}
		if len(msg.DataRecords) == 0 || len(msg.DataRecords) > nfv9MaxRecords {
			t.Fatalf("bad data record count %d", len(msg.DataRecords))
		}
		// v9 sequence numbers count exported packets, not records
		if first {
			expectedSeq = msg.Header.SequenceNumber
			first = false
		}
		if msg.Header.SequenceNumber != expectedSeq {
			t.Fatalf("bad sequence number %d != %d", msg.Header.SequenceNumber, expectedSeq)
		}
		expectedSeq++

		// the ingester attaches exactly the templates the records need,
		// make sure that lookup succeeds against the parsed session too
		if _, err = s.LookupTemplateRecords(msg); err != nil {
			t.Fatalf("template lookup failed: %v", err)
		}

		interp := ipfix.NewInterpreter(s)
		for j, dr := range msg.DataRecords {
			fields := interp.Interpret(dr)
			if len(fields) != len(nfv9V4Template.FieldSpecifiers) {
				t.Fatalf("record %d interpreted to %d fields", j, len(fields))
			}
			vals := make(map[string]any, len(fields))
			for _, f := range fields {
				if f.Name == `` || f.Value == nil {
					t.Fatalf("record %d field %d not interpretable: %+v", j, f.FieldID, f)
				}
				vals[f.Name] = f.Value
			}
			var srcKey, dstKey string
			var alen int
			switch dr.TemplateID {
			case nfv9V4TemplateID:
				srcKey, dstKey, alen = `IPV4_SRC_ADDR`, `IPV4_DST_ADDR`, 4
			case nfv9V6TemplateID:
				srcKey, dstKey, alen = `IPV6_SRC_ADDR`, `IPV6_DST_ADDR`, 16
			default:
				t.Fatalf("record %d has unexpected template %d", j, dr.TemplateID)
			}
			for _, k := range []string{srcKey, dstKey} {
				ip, ok := vals[k].(*net.IP)
				if !ok || ip == nil || len(*ip) != alen {
					t.Fatalf("record %d bad %s: %v", j, k, vals[k])
				}
			}
			proto, ok := vals[`PROTOCOL`].(uint8)
			if !ok || (proto != 1 && proto != 6 && proto != 17) {
				t.Fatalf("record %d bad protocol %v", j, vals[`PROTOCOL`])
			}
			start, sok := vals[`FIRST_SWITCHED`].(uint32)
			end, eok := vals[`LAST_SWITCHED`].(uint32)
			if !sok || !eok {
				t.Fatalf("record %d bad flow timestamps: %v %v", j, vals[`FIRST_SWITCHED`], vals[`LAST_SWITCHED`])
			}
			if start > end {
				t.Fatalf("record %d flow start after end: %v > %v", j, start, end)
			}
			if end > msg.Header.SysUptime {
				t.Fatalf("record %d flow end after export uptime: %v > %v", j, end, msg.Header.SysUptime)
			}
		}
	}
}

// TestNetflowV9WireFormat checks the bytes directly against the RFC 3954
// layout rather than through the library that produced them, so an encoding
// mistake shared by the marshaller and the parser cannot hide.
func TestNetflowV9WireFormat(t *testing.T) {
	seedVars(128)
	nfv9Reset()
	recLen := map[uint16]int{
		nfv9V4TemplateID: nfv9RecordLen(nfv9V4Template),
		nfv9V6TemplateID: nfv9RecordLen(nfv9V6Template),
	}
	ts := time.Date(2026, 7, 19, 12, 34, 56, 0, time.UTC)
	for i := 0; i < 256; i++ {
		ts = ts.Add(time.Second)
		b := genDataNetflowV9(ts)
		if len(b) < nfv9HeaderLen {
			t.Fatalf("message shorter than a v9 header: %d", len(b))
		}
		// the package doc promises one UDP datagram per entry, so it has
		// to fit inside an un-fragmented one
		if len(b) > 1472 {
			t.Fatalf("message of %d bytes will fragment a 1500 MTU datagram", len(b))
		}

		// header, RFC 3954 section 5.1
		if v := binary.BigEndian.Uint16(b[0:2]); v != 9 {
			t.Fatalf("version field is %d", v)
		}
		count := binary.BigEndian.Uint16(b[2:4])
		if up := binary.BigEndian.Uint32(b[4:8]); up == 0 {
			t.Fatal("SysUptime field is zero")
		}
		if et := binary.BigEndian.Uint32(b[8:12]); et != uint32(ts.Unix()) {
			t.Fatalf("ExportTime field is %d, want %d", et, ts.Unix())
		}
		if sid := binary.BigEndian.Uint32(b[16:20]); sid != nfv9SourceID {
			t.Fatalf("SourceID field is %d, want %d", sid, nfv9SourceID)
		}

		// FlowSets: the template set leads, data sets follow
		var records, dataSets int
		var sawTemplateSet bool
		for off := nfv9HeaderLen; off < len(b); {
			if off+4 > len(b) {
				t.Fatalf("FlowSet header at %d runs off the end of %d bytes", off, len(b))
			}
			id := binary.BigEndian.Uint16(b[off : off+2])
			l := int(binary.BigEndian.Uint16(b[off+2 : off+4]))
			if l < 4 || off+l > len(b) {
				t.Fatalf("FlowSet %d at %d has bad length %d", id, off, l)
			}
			switch {
			case id == 0: // v9 template FlowSet, note that IPFIX uses 2
				if sawTemplateSet {
					t.Fatal("more than one template FlowSet")
				} else if off != nfv9HeaderLen {
					t.Fatalf("template FlowSet at %d, want it first", off)
				}
				sawTemplateSet = true
				records += 2 // both templates ride in this one set
			case id >= 256:
				rl, ok := recLen[id]
				if !ok {
					t.Fatalf("data FlowSet for unknown template %d", id)
				}
				if (l-4)%rl != 0 {
					t.Fatalf("FlowSet %d length %d is not a header plus whole %d byte records", id, l, rl)
				}
				records += (l - 4) / rl
				dataSets++
			default:
				t.Fatalf("unexpected FlowSet id %d", id)
			}
			off += l
		}
		if !sawTemplateSet {
			t.Fatal("no template FlowSet")
		}
		if dataSets == 0 || dataSets > 2 {
			t.Fatalf("%d data FlowSets, want one per address family at most", dataSets)
		}
		// the v9 count field covers template and data records alike
		if int(count) != records {
			t.Fatalf("header count is %d, walked %d records", count, records)
		}
	}
}

// TestNetflowV9UptimeSpan walks a span far longer than the ~49.7 days a uint32
// millisecond uptime can hold, and checks that we reboot the exporter rather
// than letting the field truncate.
func TestNetflowV9UptimeSpan(t *testing.T) {
	seedVars(128)
	nfv9Reset()
	ts := time.Now().Add(-120 * 24 * time.Hour)
	var prevUptime, prevSeq uint32
	var reboots int
	for i := 0; i < 4096; i++ {
		b := genDataNetflowV9(ts)
		uptime := binary.BigEndian.Uint32(b[4:8])
		seq := binary.BigEndian.Uint32(b[12:16])
		if i > 0 && uptime < prevUptime {
			// only an explicit reboot may move uptime backwards, and it
			// has to restart the export sequence with it
			if seq != 0 {
				t.Fatalf("iteration %d: uptime fell from %d to %d without a sequence reset (seq %d)",
					i, prevUptime, uptime, seq)
			}
			reboots++
		} else if i > 0 && seq != prevSeq+1 {
			t.Fatalf("iteration %d: sequence jumped from %d to %d", i, prevSeq, seq)
		}
		prevUptime, prevSeq = uptime, seq

		// flow switch times must stay in a plausible window below the
		// header uptime, not collapse to zero because uptime truncated
		s := ipfix.NewSession()
		msg, err := s.ParseBuffer(b)
		if err != nil {
			t.Fatalf("iteration %d: parse failed: %v", i, err)
		}
		interp := ipfix.NewInterpreter(s)
		for j, dr := range msg.DataRecords {
			vals := nfv9Values(t, interp.Interpret(dr))
			first := vals[`FIRST_SWITCHED`].(uint32)
			last := vals[`LAST_SWITCHED`].(uint32)
			if first > last || last > uptime {
				t.Fatalf("iteration %d record %d: %d/%d against uptime %d", i, j, first, last, uptime)
			}
			// uptime is always well past a flow's worth of milliseconds
			// here, so nothing should be riding the underflow guards
			if uptime > 2*nfv9MaxFlowMS && (first == 0 || last == 0) {
				t.Fatalf("iteration %d record %d: switch times collapsed to zero at uptime %d", i, j, uptime)
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

// TestNetflowV9Branches makes sure every branch of the record generator is
// actually reachable.  math/rand is auto seeded, so this asserts coverage
// across a long run rather than relying on any single draw.
func TestNetflowV9Branches(t *testing.T) {
	seedVars(128)
	nfv9Reset()
	seen := map[string]bool{}
	ts := time.Date(2026, 7, 19, 12, 34, 56, 0, time.UTC)
	for i := 0; i < 512; i++ {
		ts = ts.Add(time.Second)
		s := ipfix.NewSession()
		msg, err := s.ParseBuffer(genDataNetflowV9(ts))
		if err != nil {
			t.Fatalf("parse failed: %v", err)
		}
		interp := ipfix.NewInterpreter(s)
		for _, dr := range msg.DataRecords {
			vals := nfv9Values(t, interp.Interpret(dr))
			switch dr.TemplateID {
			case nfv9V4TemplateID:
				seen[`v4`] = true
				if (*vals[`IPV4_NEXT_HOP`].(*net.IP)).IsUnspecified() {
					seen[`no next hop`] = true
				} else {
					seen[`next hop`] = true
				}
			case nfv9V6TemplateID:
				seen[`v6`] = true
				if (*vals[`IPV6_NEXT_HOP`].(*net.IP)).IsUnspecified() {
					seen[`no next hop`] = true
				} else {
					seen[`next hop`] = true
				}
			}
			switch vals[`PROTOCOL`].(uint8) {
			case 1:
				seen[`icmp`] = true
			case 6:
				seen[`tcp`] = true
				if vals[`TCP_FLAGS`].(uint8) != 0 {
					seen[`flags`] = true
				}
			case 17:
				seen[`udp`] = true
			}
			if vals[`TOS`].(uint8) == 0 {
				seen[`no tos`] = true
			} else {
				seen[`tos`] = true
			}
		}
	}
	for _, br := range []string{`v4`, `v6`, `icmp`, `tcp`, `udp`, `flags`,
		`next hop`, `no next hop`, `tos`, `no tos`} {
		if !seen[br] {
			t.Errorf("no %q record generated in 512 messages, branch may be unreachable", br)
		}
	}
}

// TestNetflowV9IpfixShareSession feeds v9 and IPFIX messages through a single
// ipfix.Session, which is what the netflow ingester does when both land on the
// same (domain, source address) key.  Both generators use template ids 256 and
// 257 with different layouts, so this pins the property that makes that safe:
// every message carries its own templates ahead of its own data FlowSets.
func TestNetflowV9IpfixShareSession(t *testing.T) {
	seedVars(128)
	nfv9Reset()
	s := ipfix.NewSession()
	ts := time.Date(2026, 7, 19, 12, 34, 56, 0, time.UTC)
	for i := 0; i < 64; i++ {
		ts = ts.Add(time.Second)
		for _, tc := range []struct {
			name    string
			data    []byte
			fields  int
			srcName string
		}{
			{`netflowv9`, genDataNetflowV9(ts), len(nfv9V4Template.FieldSpecifiers), `IPV4_SRC_ADDR`},
			{`ipfix`, genDataIPFIX(ts), len(ipfixV4Template.FieldSpecifiers), `sourceIPv4Address`},
		} {
			msg, err := s.ParseBuffer(tc.data)
			if err != nil {
				t.Fatalf("%s: parse in shared session failed: %v", tc.name, err)
			}
			if _, err = s.LookupTemplateRecords(msg); err != nil {
				t.Fatalf("%s: template lookup in shared session failed: %v", tc.name, err)
			}
			interp := ipfix.NewInterpreter(s)
			for j, dr := range msg.DataRecords {
				fields := interp.Interpret(dr)
				if len(fields) != tc.fields {
					t.Fatalf("%s record %d decoded to %d fields, want %d: templates collided",
						tc.name, j, len(fields), tc.fields)
				}
				if dr.TemplateID == 256 && fields[0].Name != tc.srcName {
					t.Fatalf("%s record %d first field is %q, want %q: templates collided",
						tc.name, j, fields[0].Name, tc.srcName)
				}
			}
		}
	}
}

// TestNetflowV9Registered checks the generator is wired into both maps, it is
// easy to add a type to dataTypes and forget finalizers
func TestNetflowV9Registered(t *testing.T) {
	dg, f, ok := getGenerator(`netflowv9`)
	if !ok {
		t.Fatal("netflowv9 is not a registered generator")
	} else if dg == nil {
		t.Fatal("netflowv9 has a nil DataGen")
	} else if f == nil {
		t.Fatal("netflowv9 has a nil Finalizer")
	}
	for _, v := range getList() {
		if v == `netflowv9` {
			return
		}
	}
	t.Fatal("netflowv9 is missing from the generator list")
}

// nfv9Values indexes interpreted fields by name, failing if any field did not
// interpret cleanly against the v9 dictionary
func nfv9Values(t *testing.T, fields []ipfix.InterpretedField) map[string]interface{} {
	t.Helper()
	vals := make(map[string]interface{}, len(fields))
	for _, f := range fields {
		if f.Name == `` || f.Value == nil {
			t.Fatalf("field %d not interpretable: %+v", f.FieldID, f)
		}
		vals[f.Name] = f.Value
	}
	return vals
}
