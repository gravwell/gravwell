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
	return []byte(ts.Format(time.RFC3339Nano))
}

func finEnumeratedValue(ent *entry.Entry) {
	ent.AddEnumeratedValueEx(`user`, getUser())
	for _, gp := range getGroups() {
		ent.AddEnumeratedValueEx(`group`, gp)
	}
	ent.AddEnumeratedValueEx(`class`, uint16(rand.Int()%0xffff))
	ent.AddEnumeratedValueEx(`id`, int64(rand.Int()))
	ent.AddEnumeratedValueEx(`ip`, v4gen.IP())
	ent.AddEnumeratedValueEx(`delay`, time.Since(ent.TS.StandardTime()))
	ent.AddEnumeratedValueEx(`now`, time.Now())
	ent.AddEnumeratedValueEx(`stuff`, rd.Paragraph())
}
