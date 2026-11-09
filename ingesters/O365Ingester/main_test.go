/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const testToken = "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiJ9.test-access-token"

// o365StyleError rebuilds the error that github.com/gravwell/o365 apiRequest
// returns on a non-200 status, including the request carrying the bearer token.
func o365StyleError(t *testing.T, body string) error {
	t.Helper()
	req, err := http.NewRequest("GET", "https://manage.office.com/api/v1.0/tenant/activity/feed/subscriptions/content?contentType=Audit.General", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	res := &http.Response{
		Status:     "403 Forbidden",
		StatusCode: 403,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
	return fmt.Errorf("non-200 status during api request.\n\tnewly enabled or newly subscribed feeds can take 12 hours or more to provide data.\n\tconfirm audit log searching is enabled for the target tenancy (https://docs.microsoft.com/en-us/microsoft-365/compliance/turn-audit-log-search-on-or-off#turn-on-audit-log-search).\n\treq: %v\n\tres: %v\n\t%v", req, res, body)
}

func TestScrubO365ErrorAPIRequest(t *testing.T) {
	const body = `{"error":{"code":"AF20022","message":"No subscription found for the specified content type"}}`
	err := o365StyleError(t, body)
	if !strings.Contains(err.Error(), testToken) {
		t.Fatal("test setup: unscrubbed error should contain the token")
	}
	s := scrubO365Error(err).Error()
	if strings.Contains(s, testToken) || strings.Contains(s, "eyJ") {
		t.Fatalf("scrubbed error still contains the token: %q", s)
	}
	if strings.Contains(s, "req: ") || strings.Contains(s, "res: ") {
		t.Fatalf("scrubbed error still contains the request/response dump: %q", s)
	}
	for _, want := range []string{"non-200 status during api request.", "12 hours", body} {
		if !strings.Contains(s, want) {
			t.Fatalf("scrubbed error lost %q: %q", want, s)
		}
	}
}

func TestScrubO365ErrorTokenElsewhere(t *testing.T) {
	for _, in := range []string{
		"map[Authorization:[Bearer " + testToken + "]]",
		`header "authorization: bearer ` + testToken + `"`,
	} {
		s := scrubO365Error(errors.New(in)).Error()
		if strings.Contains(s, testToken) {
			t.Fatalf("token not redacted from %q: %q", in, s)
		}
	}
}

func TestScrubO365ErrorPassthrough(t *testing.T) {
	if scrubO365Error(nil) != nil {
		t.Fatal("nil should pass through")
	}
	const msg = "start (a) must be before end (b)"
	if s := scrubO365Error(errors.New(msg)).Error(); s != msg {
		t.Fatalf("unrelated error changed: %q", s)
	}
}
