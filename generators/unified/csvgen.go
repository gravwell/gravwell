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

func genDataCSV(ts time.Time) []byte {
	ipa, ipb := ips()
	return []byte(fmt.Sprintf("%s,%s,%d,%s,"+
		"%s,%d,%s,%d,"+
		"\"%s\n%s\", \"%s\",%s,%x",
		ts.Format(tsFormat), getApp(), rand.Intn(0xffff), uuid.New(),
		ipa, 2048+rand.Intn(0xffff-2048), ipb, 1+rand.Intn(2047),
		rd.Paragraph(), rd.FirstName(rd.RandomGender), rd.Country(rd.TwoCharCountry), rd.City(),
		[]byte(v6gen.IP())))
}

func ips() (string, string) {
	if (rand.Int() & 3) == 0 {
		//more IPv4 than 6
		return v6gen.IP().String(), v6gen.IP().String()
	}
	return v4gen.IP().String(), v4gen.IP().String()
}
