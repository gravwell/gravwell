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
	thinkstIncidentsDomain  = "/api/v1/incidents/all"
	thinkstAuditTrailDomain = "/api/v1/audit_trail/fetch"
	thinkstEmptySleepDur    = 15 * time.Second // length of time we sleep when no results are returned at all
	thinkstSTDTimeFormat    = "2025-04-07 16:26:45 UTC+0000"
)

/*
Design the Duo Config here. There are no global arguments so everything must live here.
*/
type ThinkstConf struct {
	StartTime    time.Time
	ThinkstAPI   string
	Domain       string
	Token        string
	Tag_Name     string
	Preprocessor []string
	RateLimit    int
}

/*
Build the verify bits for the above conf.
*/
func (c cfgType) ThinkstVerify() error {

	for k, v := range c.ThinkstConf {
		if err := c.Preprocessor.Validate(); err != nil {
			return err
		}
		if v.Tag_Name == "" {
			return errors.New("Tag-Name not specified")
		}
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("Listener %s preprocessor invalid: %v", k, err)
		}
		if v.StartTime.IsZero() {
			v.StartTime = time.Now()
		}
		if v.Domain == "" {
			return errors.New("Thinkst Domain not specified")
		}
		if v.Token == "" {
			return errors.New("Thinkst Key not specified")
		}
		if !IsValidThinkstAPI(v.ThinkstAPI) {
			return errors.New("Thinkst API is not valid")
		}

	}
	return nil
}

func IsValidThinkstAPI(thinkstAPI string) bool {
	switch thinkstAPI {
	case
		"audit",
		"incident":
		return true
	}
	return false
}
