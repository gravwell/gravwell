/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"fmt"
	"math/rand"
	"time"

	rd "github.com/Pallinder/go-randomdata"
)

func genDataFields(ts time.Time) []byte {
	ipa, ipb := ips()
	return []byte(fmt.Sprintf("%s%s%s%s%s%s%d%s%s%s%d%s\"%s\"",
		ts.Format(tsFormat), delim, getApp(), delim,
		ipa, delim, 1+rand.Intn(2048), delim,
		ipb, delim, 2048+rand.Intn(0xffff-2048), delim,
		rd.Paragraph()))
}
