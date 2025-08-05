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
	"log"
	"time"
)

/*
//Put any constants here that are needed only for duo
*/
const (
	oktaSystemLogsPath          = `/api/v1/logs`
	oktaUserLogsPath            = `/api/v1/users`
	oktaDefaultTimeout          = time.Second * 3
	oktaDefaultBackoff          = time.Second * 10
	oktaDefaultPageSize         = 100
	oktaDefaultRequestPerMinute = 60
	oktaDefaultRequestBurst     = 10
	oktaDefaultRequestTimeout   = 20 * time.Second
	oktaPartialSleepDur         = 3 * time.Second  // length of time we sleep when a partial results page is returned
	oktaUserLogWindowSize       = 10 * time.Minute // how often do we check for updated users
	oktaUserLogWindowLag        = time.Minute      // how far do we lag the window when taking samples
	oktaEmptySleepDur           = 15 * time.Second // length of time we sleep when no results are returned at all
)

/*
Design the Duo Config here. There are no global arguments so everything must live here.
*/
type OktaConf struct {
	StartTime     string
	OktaDomain    string
	OktaToken     string
	UserTag       string
	BatchSize     int
	MaxBurstSize  int    //Number of requests per page
	SeedUsers     bool   //Acquire full user list at startup
	SeedUserStart string //timestamp to start user list window (RFC3339)
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
		if v.StartTime == `` {
			v.StartTime = time.Now().String()
		}
		if v.OktaDomain == "" {
			return errors.New("OktaDomain not specified")
		}
		if v.OktaToken == "" {
			return errors.New("OktaToken not specified")
		}
		if v.BatchSize < 1 || v.BatchSize > 3000 {
			log.Fatal("invalid batch-size, must be > 0 and < 3000", v.BatchSize)
		}

	}
	return nil
}
