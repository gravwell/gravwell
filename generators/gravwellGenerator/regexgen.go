/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
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
	"github.com/google/uuid"
)

func genDataRegex(ts time.Time) []byte {
	ipa, ipb := ips()
	return []byte(fmt.Sprintf("%s [%s] <%s> %s %d %s %d /%s/%s/%s/%s.%s %s {%s}",
		ts.Format(tsFormat), getApp(), uuid.New(),
		ipa, 2048+rand.Intn(0xffff-2048), ipb, 1+rand.Intn(2047),
		rd.LastName(), rd.FirstName(0), rd.FirstName(1), rd.Noun(), rd.Locale(),
		rd.UserAgentString(), rd.Email()))
}
