/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"net"
	"testing"
	"time"

	"github.com/gravwell/ipfix"
)

func TestGenDataNetflowV9(t *testing.T) {
	seedVars(128)
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
		for _, tr := range msg.TemplateRecords {
			if len(tr.FieldSpecifiers) != len(nfv9V4Template.FieldSpecifiers) {
				t.Fatalf("template %d has %d field specifiers", tr.TemplateID, len(tr.FieldSpecifiers))
			}
		}
		if len(msg.DataRecords) == 0 || len(msg.DataRecords) > nfv9MaxRecords {
			t.Fatalf("bad data record count %d", len(msg.DataRecords))
		}
		// the v9 header count covers template and data records alike
		if int(msg.Header.Length) != len(msg.TemplateRecords)+len(msg.DataRecords) {
			t.Fatalf("bad record count %d != %d", msg.Header.Length,
				len(msg.TemplateRecords)+len(msg.DataRecords))
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
