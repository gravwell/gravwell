/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"log"
	"math/rand"
	"time"

	rd "github.com/Pallinder/go-randomdata"
	"github.com/goccy/go-json"
	//"github.com/bet365/jingo"
	"github.com/gravwell/gravwell/v3/generators/ipgen"
)

const (
	streamBlock = 10
)

type datum struct {
	//TS        time.Time `json:"time"`
	TS        string   `json:"time"`
	Account   Account  `json:"account"`
	Class     int      `json:"class"`
	Groups    []string `json:"groups,omitempty"`
	UserAgent string   `json:"user_agent"`
	IP        string   `json:"ip"`
	Data      string   `json:"data,escape"`
}

var (
	//enc   = jingo.NewStructEncoder(datum{})
	v4gen *ipgen.V4Gen
)

func init() {
	var err error
	v4gen, err = ipgen.RandomWeightedV4Generator(40)
	if err != nil {
		log.Fatalf("Failed to instantiate v4 generator: %v", err)
	}
}

// genData creates a marshalled JSON buffer
// the jingo encoder is faster, but because we throw the buffers into our entries
// and hand them into the ingest muxer we can't really track those buffers so we won't get the benefit
// of the buffered pool.  The encoder is still about 3X faster than the standard library encoder
func genData(ts time.Time) (r []byte) {
	//bb := jingo.NewBufferFromPool()
	var d datum
	//d.TS = ts //for stdlib json encoder
	d.TS = ts.UTC().Format(time.RFC3339)
	d.Class = rand.Int() % 0xffff
	d.Data = rd.Paragraph()
	d.Groups = getGroups()
	d.Account = getUser()
	d.UserAgent = rd.UserAgentString()
	d.IP = v4gen.IP().String()
	r, _ = json.Marshal(&d)
	//r = append(r, bb.Bytes...) //copy out of the pool
	//bb.ReturnToPool()
	return
}
