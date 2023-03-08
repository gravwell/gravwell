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
	"time"

	rd "github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func genDataEnumeratedValue(ts time.Time) []byte {
	return []byte(`CheckEVs, nothing interesting here`)
}

// this roughly matches the json structure
func finEnumeratedValue(ent *entry.Entry) {
	ent.AddEnumeratedValueEx(`ts`, ent.TS)
	//fill in the User/Account
	u := getUser()
	ent.AddEnumeratedValueEx(`user`, u.User)
	ent.AddEnumeratedValueEx(`name`, u.Name)
	ent.AddEnumeratedValueEx(`email`, u.Email)
	ent.AddEnumeratedValueEx(`phone`, u.Phone)
	ent.AddEnumeratedValueEx(`address`, u.Address)
	ent.AddEnumeratedValueEx(`state`, u.State)
	ent.AddEnumeratedValueEx(`country`, u.Country)

	//add other items
	ent.AddEnumeratedValueEx(`class`, uint16(rand.Int()%0xffff))
	for _, gp := range getGroups() {
		ent.AddEnumeratedValueEx(`group`, gp)
	}
	ent.AddEnumeratedValueEx(`useragent`, rd.UserAgentString())
	ent.AddEnumeratedValueEx(`ip`, v4gen.IP())
	ent.AddEnumeratedValueEx(`data`, rd.Paragraph())

	// a few extras for testing
	ent.AddEnumeratedValueEx(`delay`, time.Since(ent.TS.StandardTime()))
	ent.AddEnumeratedValueEx(`now`, time.Now())
}
