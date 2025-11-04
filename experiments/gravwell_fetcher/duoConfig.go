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
// Put any constants here that are needed only for Duo
*/
const (
	duoAccountLog        = "/admin/v1/info/summary"
	duoAdminLog          = "/admin/v1/logs/administrator"
	duoActivityLog       = "/admin/v2/logs/activity"
	duoTelephonyLog      = "/admin/v2/logs/telephony"
	duoAuthenticationLog = "/admin/v2/logs/authentication"
	duoEndpointLog       = "/admin/v1/registered_devices"
	duoTrustMonitorLog   = "/admin/v1/trust_monitor/events"

	// Length of time we sleep when no results are returned at all
	duoEmptySleepDur = 30 * time.Second
)

/*
q
Design the Duo Config here. There are no global arguments so everything must live here.
*/
type duoConf struct {
	StartTime    time.Time
	Domain       string
	Key          string
	Secret       string
	DuoAPI       string
	Tag_Name     string
	Preprocessor []string
	RateLimit    int
}

/*
Build the verify bits for the above conf.
*/
func (c cfgType) DuoVerify() error {
	for k, v := range c.DuoConf {
		// validate preprocessor section first so tag errors point to the right listener
		if err := c.Preprocessor.Validate(); err != nil {
			return err
		}
		if v.Tag_Name == "" {
			return errors.New("Tag-Name not specified")
		}
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("listener %s preprocessor invalid: %v", k, err)
		}

		// Duo creds + endpoint basics
		if v.Domain == "" {
			return errors.New("Duo Domain not specified")
		}
		if v.Key == "" {
			return errors.New("Duo Key not specified")
		}
		if v.Secret == "" {
			return errors.New("Duo Secret not specified")
		}
		if v.DuoAPI == "" {
			return errors.New("Duo API not specified")
		}
		if !IsValidDuoAPI(v.DuoAPI) {
			return fmt.Errorf("Duo API is not valid: %q", v.DuoAPI)
		}

		// Optional: sanity for ratelimit (allow 0 = use default)
		if v.RateLimit < 0 {
			return fmt.Errorf("RateLimit must be >= 0 for listener %s", k)
		}
	}
	return nil
}

func IsValidDuoAPI(duoAPI string) bool {
	switch duoAPI {
	case
		"account",
		"admin",
		"activity",
		"telephony",
		"authentication",
		"endpoint",
		"trust":
		return true
	}
	return false
}