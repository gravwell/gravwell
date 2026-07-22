/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestCEFEscapeHeader(t *testing.T) {
	tests := []struct {
		in, out string
	}{
		{`plain`, `plain`},
		{`has|pipe`, `has\|pipe`},
		{`has\slash`, `has\\slash`},
		{`both\|`, `both\\\|`},
		{"new\nline", `new line`},
	}
	for _, tt := range tests {
		if got := cefHeaderReplacer.Replace(tt.in); got != tt.out {
			t.Errorf("cefHeaderReplacer.Replace(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}

func TestCEFEscapeExt(t *testing.T) {
	tests := []struct {
		in, out string
	}{
		{`plain`, `plain`},
		{`key=value`, `key\=value`},
		{`C:\dir\file`, `C:\\dir\\file`},
		{"multi\nline", `multi\nline`},
		{`pipes|ok|in|ext`, `pipes|ok|in|ext`},
	}
	for _, tt := range tests {
		if got := cefExtReplacer.Replace(tt.in); got != tt.out {
			t.Errorf("cefExtReplacer.Replace(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}

var cefRx = regexp.MustCompile(`^[A-Z][a-z]{2} [ 0-9][0-9] \d{2}:\d{2}:\d{2} \S+ CEF:0\|`)

func TestGenDataCEF(t *testing.T) {
	seedVars(128)
	ts := time.Date(2026, 7, 19, 12, 34, 56, 0, time.UTC)
	for i := 0; i < 256; i++ {
		v := string(genDataCEF(ts))
		if !cefRx.MatchString(v) {
			t.Fatalf("bad CEF prefix: %q", v)
		}
		// split off the syslog header then walk the 7 pipe delimited header fields,
		// honoring escaped pipes
		_, cef, _ := strings.Cut(v, `CEF:`)
		var fields []string
		var cur strings.Builder
		var escaped bool
		for _, r := range cef {
			switch {
			case escaped:
				cur.WriteRune(r)
				escaped = false
			case r == '\\':
				cur.WriteRune(r)
				escaped = true
			case r == '|' && len(fields) < 7:
				fields = append(fields, cur.String())
				cur.Reset()
			default:
				cur.WriteRune(r)
			}
		}
		fields = append(fields, cur.String())
		if len(fields) != 8 {
			t.Fatalf("expected 8 fields (version, 6 header, extension), got %d: %q", len(fields), v)
		}
		if fields[0] != `0` {
			t.Fatalf("bad CEF version %q in %q", fields[0], v)
		}
		for i, f := range fields[:7] {
			if f == `` {
				t.Fatalf("empty header field %d in %q", i, v)
			}
		}
		if sev := fields[6]; len(sev) > 2 || strings.Trim(sev, "0123456789") != `` {
			t.Fatalf("bad severity %q in %q", sev, v)
		}
	}
}
