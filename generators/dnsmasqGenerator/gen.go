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
	"log"
	"math/rand"
	"net"
	"time"

	"github.com/brianvoe/gofakeit"
	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/generators/ipgen"
)

var (
	v4gen *ipgen.V4Gen
)

func init() {
	var err error
	v4gen, err = ipgen.RandomWeightedV4Generator(40)
	if err != nil {
		log.Fatalf("Failed to instantiate v4 generator: %v", err)
	}
}

func reqip() string {
	r := rand.Uint32()
	return net.IPv4(172, 16+(byte(r)>>6), byte(r>>16), byte(8)).String()
}

type osc struct {
	resp   bool
	name   string
	origip string
	respip string
}

var last osc

func genData(ts time.Time) (r []byte) {
	if last.resp {
		//new request
		last.name = gofakeit.DomainName()
		last.respip = gofakeit.IPv4Address()
		last.origip = reqip()
	}

	m := rfc5424.Message{
		Priority:  30,
		Timestamp: ts,
		Hostname:  "gateway",
		AppName:   "dnsmasq",
		Message:   []byte(last.String()),
	}
	last.resp = !last.resp

	r, _ = m.MarshalBinary()
	return
}

func (o osc) String() string {
	if o.resp {
		return fmt.Sprintf("reply %s is %s", o.name, o.respip)
	}
	return fmt.Sprintf("query[A] %s from %s", o.name, o.origip)
}
