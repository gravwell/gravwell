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
	"strings"
	"time"
)

/*
//Put any constants here that are needed only for Okta
*/
const (
	oktaSystemLogsPath        = "/api/v1/logs"
	oktaUserLogsPath          = "/api/v1/users"
	oktaDefaultBackoff        = 2 * time.Second
	oktaDefaultRequestTimeout = 30 * time.Second
	oktaUserLogWindowSize = 5 * time.Minute // how often do we check for updated users
	oktaUserLogWindowLag  = 15 * time.Second // how far do we lag the window when taking samples
	oktaPartialSleepDur = 3 * time.Second // length of time we sleep when a partial results page is returned
	oktaEmptySleepDur = 15 * time.Second // length of time we sleep when no results are returned

)

/*
Design the Okta Config here. There are no global arguments so everything must live here.
*/
type OktaConf struct {
	StartTime     time.Time
	OktaDomain    string
	OktaToken     string
	BatchSize     int
	MaxBurstSize  int 	//Number of requests per page
	SeedUsers     bool  //Acquire full user list at startup
	SeedUserStart time.Time //timestamp to start user list window (RFC3339)
	Tag_Name      string
	Preprocessor  []string
	RateLimit     int
}

/*
Build the verify bits for the above conf.
*/
func (c cfgType) OktaVerify() error {
	for k, v := range c.OktaConf {
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
		if strings.TrimSpace(v.OktaDomain) == "" {
			return errors.New("OktaDomain not specified")
		}
		if strings.TrimSpace(v.OktaToken) == "" {
			return errors.New("OktaToken not specified")
		}
	}
	return nil
}