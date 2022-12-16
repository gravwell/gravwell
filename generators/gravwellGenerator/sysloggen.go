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
)

func genDataSyslog(ts time.Time) []byte {
	sev := rand.Intn(24)
	fac := rand.Intn(7)
	prio := (sev << 3) | fac
	return []byte(fmt.Sprintf("<%d>1 %s %s %s %d - %s %s",
		prio, ts.Format(tsFormat), getHost(), getApp(), rand.Intn(0xffff), genStructData(), rd.Paragraph()))
}

func genStructData() string {
	return fmt.Sprintf(`[email="%s" source-address="%s" source-port=%d destination-address="%s" destination-port=%d useragent="%s"]`, rd.Email(), v4gen.IP().String(), 0x2000+rand.Intn(0xffff-0x2000), v4gen.IP().String(), 1+rand.Intn(2047), rd.UserAgentString())
}
