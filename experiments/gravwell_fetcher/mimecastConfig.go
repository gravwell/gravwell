/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"fmt"
	"time"
)

/*
Mimecast-specific constants
*/
const (
	// Base
	mimecastAuthBaseDomain = "https://api.services.mimecast.com"

	// General behavior
	mimecastEmptySleepDur = 600 * time.Second // sleep when no results

	// Time formats
	// Audit endpoints require ISO-8601 with timezone like 2011-12-03T10:15:30+0000
	// (matches Mimecast docs)
	mimecastDateTimeFormat = "2006-01-02T15:04:05-0700"

	// Batch SIEM (CG) range params. If you hit format errors, consider RFC3339:
	//   "2006-01-02T15:04:05Z07:00"
	// For now we keep date-only to match current working calls.
	mimecastMTADateTimeFormat = "2006-01-02"

	// Endpoints
	mimecastAuditAPI         = "/api/audit/get-audit-events"
	mimecastSIEMAPI          = "/siem/v1/batch/events/cg"
	mimecastMonitoringURLAPI = "/api/ttp/url/get-logs"
	mimecastMonitoringAttAPI = "/api/ttp/attachment/get-logs"
	mimecastMonitoringImpAPI = "/api/ttp/impersonation/get-logs"
	mimecastMonitoringDLPAPI = "/api/dlp/get-logs"

	// Misc
	mimecastMTAPageSize = "50"
)

/*
Mimecast configuration stanza
*/
type MimecastConf struct {
	StartTime    time.Time
	ClientID     string
	ClientSecret string
	MimecastAPI  string
	Tag_Name     string
	Preprocessor []string
	RateLimit    int
}

/*
Verify & normalize
*/
func (c cfgType) MimecastVerify() error {
	for k, v := range c.MimecastConf {
		// preprocessor set validation
		if err := c.Preprocessor.Validate(); err != nil {
			return err
		}
		if v.Tag_Name == "" {
			return errors.New("Tag-Name not specified")
		}
		if v.ClientID == "" {
			return errors.New("ClientID not specified")
		}
		if v.ClientSecret == "" {
			return errors.New("ClientSecret not specified")
		}
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("listener %s preprocessor invalid: %v", k, err)
		}
		// default StartTime if not provided; assign back so it persists
		if v.StartTime.IsZero() {
			v.StartTime = time.Now()
			c.MimecastConf[k] = v
		}
		// validate requested endpoint
		if !IsValidMimecastAPI(v.MimecastAPI) {
			return fmt.Errorf("Mimecast API is not valid for listener %s: %q", k, v.MimecastAPI)
		}
	}
	return nil
}

func IsValidMimecastAPI(mimecastApi string) bool {
	switch mimecastApi {
	case
		"audit",
		"mta-url",
		"mta-attachment",
		"mta-delivery",
		"mta-receipt",
		"mta-process",
		"mta-av",
		"mta-spam",
		"mta-internal",
		"mta-journal",
		"mta-impersonation":
		return true
	}
	return false
}
