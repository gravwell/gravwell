/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"fmt"
	"time"
)

/*
//Put any constants here that are needed only for shodan
*/
const (
	shodanHostDomain    = "/shodan/host/"
	shodanSearchDomain  = "/shodan/host/search"
	shodanCountDomain   = "/shodan/host/count"
	shodanEmptySleepDur = 15 * time.Second // length of time we sleep when no results are returned at all
)

/*
Design the Shodan Config here. There are no global arguments so everything must live here.
*/
type ShodanConf struct {
	StartTime    time.Time
	ShodanAPI    string
	Domain       string
	Token        string
	Tag_Name     string
	Preprocessor []string
	RateLimit    int
	Query        string
}

/*
Build the verify bits for the above conf.
*/
func (c cfgType) ShodanVerify() error {
	for _, v := range c.ShodanConf {
		if v.StartTime.IsZero() {
			v.StartTime = time.Now()
		}
		if v.ShodanAPI == "" {
			return fmt.Errorf("Shodan API type not specified")
		}
		if !IsValidShodanAPI(v.ShodanAPI) {
			return fmt.Errorf("Invalid Shodan API type: %s", v.ShodanAPI)
		}
		if v.Domain == "" {
			return fmt.Errorf("Domain not specified")
		}
		if v.Token == "" {
			return fmt.Errorf("Token not specified")
		}
		if v.Tag_Name == "" {
			return fmt.Errorf("Tag name not specified")
		}
		if v.Query == "" && (v.ShodanAPI == "search" || v.ShodanAPI == "count") {
			return fmt.Errorf("Query not specified for search/count API")
		}
	}
	return nil
}

func IsValidShodanAPI(api string) bool {
	switch api {
	case "host", "search", "count":
		return true
	default:
		return false
	}
}
