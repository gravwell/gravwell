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

func TestGenDataIPFIX(t *testing.T) {
	seedVars(128)
	ts := time.Date(2026, 7, 19, 12, 34, 56, 0, time.UTC)
	var expectedSeq uint32
	first := true
	for i := 0; i < 256; i++ {
		ts = ts.Add(time.Second)
		b := genDataIPFIX(ts)

		// each entry must stand on its own: a brand new session with no
		// prior template knowledge has to fully decode it
		s := ipfix.NewSession()
		msg, err := s.ParseBuffer(b)
		if err != nil {
			t.Fatalf("fresh session failed to parse message: %v", err)
		}
		if msg.Header.Version != 10 {
			t.Fatalf("bad version %d", msg.Header.Version)
		}
		if msg.Header.ExportTime != uint32(ts.Unix()) {
			t.Fatalf("bad export time %d != %d", msg.Header.ExportTime, ts.Unix())
		}
		if len(msg.TemplateRecords) != 2 {
			t.Fatalf("expected 2 template records, got %d", len(msg.TemplateRecords))
		}
		for _, tr := range msg.TemplateRecords {
			if len(tr.FieldSpecifiers) != len(ipfixV4Template.FieldSpecifiers) {
				t.Fatalf("template %d has %d field specifiers", tr.TemplateID, len(tr.FieldSpecifiers))
			}
		}
		if len(msg.DataRecords) == 0 || len(msg.DataRecords) > ipfixMaxRecords {
			t.Fatalf("bad data record count %d", len(msg.DataRecords))
		}
		if first {
			expectedSeq = msg.Header.SequenceNumber
			first = false
		}
		if msg.Header.SequenceNumber != expectedSeq {
			t.Fatalf("bad sequence number %d != %d", msg.Header.SequenceNumber, expectedSeq)
		}
		expectedSeq += uint32(len(msg.DataRecords))

		// the ingester attaches exactly the templates the records need,
		// make sure that lookup succeeds against the parsed session too
		if _, err = s.LookupTemplateRecords(msg); err != nil {
			t.Fatalf("template lookup failed: %v", err)
		}

		interp := ipfix.NewInterpreter(s)
		for j, dr := range msg.DataRecords {
			fields := interp.Interpret(dr)
			if len(fields) != len(ipfixV4Template.FieldSpecifiers) {
				t.Fatalf("record %d interpreted to %d fields", j, len(fields))
			}
			vals := make(map[string]interface{}, len(fields))
			for _, f := range fields {
				if f.Name == `` || f.Value == nil {
					t.Fatalf("record %d field %d not interpretable: %+v", j, f.FieldID, f)
				}
				vals[f.Name] = f.Value
			}
			var srcKey, dstKey string
			var alen int
			switch dr.TemplateID {
			case ipfixV4TemplateID:
				srcKey, dstKey, alen = `sourceIPv4Address`, `destinationIPv4Address`, 4
			case ipfixV6TemplateID:
				srcKey, dstKey, alen = `sourceIPv6Address`, `destinationIPv6Address`, 16
			default:
				t.Fatalf("record %d has unexpected template %d", j, dr.TemplateID)
			}
			for _, k := range []string{srcKey, dstKey} {
				ip, ok := vals[k].(*net.IP)
				if !ok || ip == nil || len(*ip) != alen {
					t.Fatalf("record %d bad %s: %v", j, k, vals[k])
				}
			}
			proto, ok := vals[`protocolIdentifier`].(uint8)
			if !ok || (proto != 1 && proto != 6 && proto != 17) {
				t.Fatalf("record %d bad protocol %v", j, vals[`protocolIdentifier`])
			}
			start, sok := vals[`flowStartMilliseconds`].(time.Time)
			end, eok := vals[`flowEndMilliseconds`].(time.Time)
			if !sok || !eok {
				t.Fatalf("record %d bad flow timestamps: %v %v", j, vals[`flowStartMilliseconds`], vals[`flowEndMilliseconds`])
			}
			if start.After(end) {
				t.Fatalf("record %d flow start after end: %v > %v", j, start, end)
			}
			if end.After(ts) {
				t.Fatalf("record %d flow end after export time: %v > %v", j, end, ts)
			}
		}
	}
}
