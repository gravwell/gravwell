/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package wiz

import "testing"

func TestParseQueryVars(t *testing.T) {
	doc := `query Foo($first: Int, $after: String, $since: DateTime) {
		auditLogEntries(first: $first, after: $after, filterBy: {timestamp: {after: $since}}) {
			nodes { id }
			pageInfo { hasNextPage endCursor }
		}
	}`
	vars := parseQueryVars(doc)
	for _, want := range []string{"first", "after", "since"} {
		if !vars[want] {
			t.Errorf("expected variable %q in %v", want, vars)
		}
	}
	if len(vars) != 3 {
		t.Errorf("parsed %d vars, want 3: %v", len(vars), vars)
	}
}

// TestBuiltinSources verifies the built-in queries wire up to the expected
// GraphQL fields and declare the pagination/filter variables the scanner
// supplies.
func TestBuiltinSources(t *testing.T) {
	want := map[string]string{
		sourceVulnerability: "vulnerabilityFindings",
		sourceIssue:         "issues",
		sourceDetection:     "detections",
		sourceConfiguration: "configurationFindings",
		sourceAudit:         "auditLogEntries",
	}
	srcs := builtinSources()
	if len(srcs) != len(want) {
		t.Fatalf("got %d sources, want %d", len(srcs), len(want))
	}
	for _, s := range srcs {
		if want[s.name] != s.field {
			t.Errorf("source %q field = %q, want %q", s.name, s.field, want[s.name])
		}
		if s.tsField == "" {
			t.Errorf("source %q missing cursor timestamp field", s.name)
		}
		vars := parseQueryVars(s.query)
		if !vars["first"] || !vars["after"] || !vars["since"] {
			t.Errorf("source %q query missing first/after/since vars: %v", s.name, vars)
		}
	}
}
