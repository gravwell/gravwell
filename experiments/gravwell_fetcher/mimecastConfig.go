/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
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
//Put any constants here that are needed only for duo
*/
const (
	mimecastAuthBaseDomain   = "https://api.services.mimecast.com"
	mimecastEmptySleepDur    = 15 * time.Second // length of time we sleep when no results are returned at all
	mimecastDateTimeFormat   = "2006-01-02T15:04:05-0700"
	mimecastAuditAPI         = "/api/audit/get-audit-events"
	mimecastSIEMAPI          = "/siem/v1/batch/events/cg"
	mimecastMonitoringURLAPI = "/api/ttp/url/get-logs"
	mimecastMonitoringAttAPI = "/api/ttp/attachment/get-logs"
	mimecastMonitoringImpAPI = "/api/ttp/impersonation/get-logs"
	mimecastMonitoringDLPAPI = "/api/dlp/get-logs"
)

/*
Design the Duo Config here. There are no global arguments so everything must live here.
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
Build the verify bits for the above conf.
*/
func (c cfgType) MimecastVerify() error {

	for k, v := range c.MimecastConf {
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
			return fmt.Errorf("Listener %s preprocessor invalid: %v", k, err)
		}
		if v.StartTime.IsZero() {
			v.StartTime = time.Now()
		}
		if !IsValidMimecastAPI(v.MimecastAPI) {
			return errors.New("Mimecast API is not valid")
		}
	}
	return nil
}
func IsValidMimecastAPI(mimecastApi string) bool {
	switch mimecastApi {
	case
		"audit",
		"url",
		"attachment",
		"impersonation":
		return true
	}
	return false
}
