/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"math/rand"

	rd "github.com/Pallinder/go-randomdata"
)

const (
	hcount   int    = 32
	appcount int    = 2048
	tsFormat string = `2006-01-02T15:04:05.999999Z07:00`
)

var (
	hosts []string
	apps  []string
)

func init() {
	for i := 0; i < hcount; i++ {
		hosts = append(hosts, rd.Noun())
	}
	for i := 0; i < appcount; i++ {
		apps = append(apps, rd.Adjective())
	}
}

func getHost() string {
	return hosts[rand.Intn(len(hosts))]
}

func getApp() string {
	return apps[rand.Intn(len(apps))]
}
