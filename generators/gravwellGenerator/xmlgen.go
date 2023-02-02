/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/xml"
	"math/rand"
	"time"

	rd "github.com/Pallinder/go-randomdata"
)

type xmldatum struct {
	TS        string   `xml:"time,attr"`
	Account   Account  `xml:"account"`
	Class     int      `xml:"class"`
	Groups    []string `xml:"groups,omitempty"`
	UserAgent string   `xml:"user_agent"`
	IP        string   `xml:"ip"`
	Data      string   `xml:"data"`
	XMLName   xml.Name `xml:"event"`
}

// genDataXML creates a marshalled XML buffer
func genDataXML(ts time.Time) (r []byte) {
	var d xmldatum
	d.TS = ts.UTC().Format(time.RFC3339)
	d.Class = rand.Int() % 0xffff
	d.Data = rd.Paragraph()
	d.Groups = getGroups()
	d.Account = getUser()
	d.UserAgent = rd.UserAgentString()
	d.IP = v4gen.IP().String()

	r, _ = xml.Marshal(&d)
	return
}
