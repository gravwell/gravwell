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
	"net"
	"time"

	rd "github.com/Pallinder/go-randomdata"
	"github.com/goccy/go-json"
)

type datum struct {
	TS        string         `json:"time"`
	Account   Account        `json:"account"`
	Class     int            `json:"class"`
	Groups    []ComplexGroup `json:"groups,omitempty"`
	UserAgent string         `json:"user_agent"`
	IP        string         `json:"ip"`
	Data      string         `json:"data,escape"`
}

type megaDatum struct {
	Account   Account        `json:"account"`
	Class     int            `json:"class"`
	Groups    []ComplexGroup `json:"groups,omitempty"`
	UserAgent string         `json:"user_agent"`
	IP        string         `json:"ip"`
	TS        int64          `json:"time"` //bury the timestamp somewhere dumb, its also encoded unix timestamp
	Records   []megaRecord   `json:"records"`
	Data      string         `json:"data,escape"`
}

type megaRecord struct {
	TS         string   `json:"record_time"`
	Flows      []flow   `json:"flows"`
	Statements []string `json:"statements,escape"`
	Agents     []string `json:"agents"`
}

type flow struct {
	Src     net.IP `json:"src"`
	Dst     net.IP `json:"dst"`
	SrcPort uint16 `json:"src_port"`
	DstPort uint16 `json:"dst_port"`
	Proto   string `json:"protocol"`
	Desc    string `json:"desc"`
}

// genDataJSON creates a marshalled JSON buffer
// the jingo encoder is faster, but because we throw the buffers into our entries
// and hand them into the ingest muxer we can't really track those buffers so we won't get the benefit
// of the buffered pool.  The encoder is still about 3X faster than the standard library encoder
func genDataJSON(ts time.Time) (r []byte) {
	var d datum
	d.TS = ts.UTC().Format(time.RFC3339)
	d.Class = rand.Int() % 0xffff
	d.Data = rd.Paragraph()
	d.Groups = getComplexGroups()
	d.Account = getUser()
	d.UserAgent = rd.UserAgentString()
	d.IP = v4gen.IP().String()
	r, _ = json.Marshal(&d)
	return
}

// genDataMegaJSON looks a lot like JSON but also includes an array of records that are huge.
// The purpose of this data type is to test ingester and renderer behavior when records are stupidly large
// these will vary from 32KB to 128KB
func genDataMegaJSON(ts time.Time) (r []byte) {
	var d megaDatum
	d.TS = ts.UTC().Unix()
	d.Class = rand.Int() % 0xffff
	d.Groups = getComplexGroups()
	d.Account = getUser()
	d.UserAgent = rd.UserAgentString()
	d.IP = v4gen.IP().String()
	d.Data = rd.Paragraph()
	d.Records = genMegaRecords(ts)
	r, _ = json.Marshal(&d)
	return

}

func genMegaRecords(ts time.Time) (r []megaRecord) {
	//pick, between 10 and 40 records, each record is going to be betwen 3 and 4 KB
	cnt := rand.Intn(30) + 10
	r = make([]megaRecord, cnt)
	for i := range r {
		r[i].TS = ts.Add(time.Duration(i) + time.Second).Format(time.RFC850)
		r[i] = genMegaRecord(ts)
	}
	return
}

func genMegaRecord(ts time.Time) (mr megaRecord) {
	mr.Agents = genAgents(rand.Intn(8))
	mr.Statements = genStatements(rand.Intn(8))
	mr.Flows = genFlows(rand.Intn(16))
	return
}

func genAgents(cnt int) (r []string) {
	if cnt <= 0 {
		r = []string{}
		return
	}

	r = make([]string, cnt)
	for i := range r {
		r[i] = rd.UserAgentString()
	}

	return
}

func genStatements(cnt int) (r []string) {
	if cnt <= 0 {
		r = []string{}
		return
	}

	r = make([]string, cnt)
	for i := range r {
		r[i] = rd.Paragraph()
	}

	return
}

func genFlows(cnt int) (r []flow) {
	if cnt <= 0 {
		r = []flow{}
		return
	}
	r = make([]flow, cnt)
	for i := range r {
		r[i] = flow{
			Src:     v4gen.IP(),
			Dst:     v4gen.IP(),
			SrcPort: uint16(rand.Intn(0xffff)),
			DstPort: uint16(rand.Intn(0xffff)),
			Proto:   getApp(),
			Desc:    rd.FullName(i & 1),
		}
	}
	return
}
