/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
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
	"time"

	"github.com/gravwell/gravwell/v3/generators/ipgen"
)

const (
	streamBlock = 10
)

var (
	v4gen *ipgen.V4Gen
	v6gen *ipgen.V6Gen
)

func init() {
	var err error
	v4gen, err = ipgen.RandomWeightedV4Generator(3)
	if err != nil {
		log.Fatal("Failed to instantiate v4 generator: %v", err)
	}
	v6gen, err = ipgen.RandomWeightedV6Generator(30)
	if err != nil {
		log.Fatal("Failed to instantiate v6 generator: %v", err)
	}
}

func genConnData(ts time.Time) []byte {
	uid := randomBase62(17)

	orig, resp := ips()

	orig_port, resp_port := ports()

	local_orig := "T"
	local_resp := "T"
	if rand.Int()%2 == 0 {
		local_orig = "F"
	}
	if rand.Int()%2 == 0 {
		local_resp = "F"
	}

	proto := protos[rand.Intn(len(protos))]
	service := "-"
	if svcs, ok := services[proto]; ok {
		service = svcs[rand.Intn(len(svcs))]
	}

	duration := float64(rand.Intn(10)) + rand.Float64()

	orig_bytes := rand.Intn(1000000000)
	resp_bytes := rand.Intn(1000000000)

	orig_pkts := (orig_bytes / (40 + rand.Intn(65000)))
	resp_pkts := (orig_bytes / (40 + rand.Intn(65000)))

	orig_ip_bytes := orig_bytes + rand.Intn(500)
	resp_ip_bytes := resp_bytes + rand.Intn(500)

	conn_state := states[rand.Intn(len(states))]

	missed_bytes := 0

	history := histories[rand.Intn(len(histories))]

	tunnel_parents := "-"

	return []byte(fmt.Sprintf("%d.%6d\t%s\t%s\t%d\t%s\t%d\t%s\t%s\t%.6f\t%v\t%v\t%v\t%v\t%v\t%v\t%v\t%v\t%v\t%v\t%v\t%v",
		ts.Unix(), ts.UnixNano()%ts.Unix(), uid,
		orig, orig_port,
		resp, resp_port,
		proto, service,
		duration,
		orig_bytes, resp_bytes,
		conn_state,
		local_orig, local_resp,
		missed_bytes,
		history,
		orig_pkts, orig_ip_bytes,
		resp_pkts, resp_ip_bytes,
		tunnel_parents,
	))
}

func ips() (string, string) {
	if *enableIPv6 && (rand.Int()&3) == 0 {
		//more IPv4 than 6
		return v6gen.IP().String(), v6gen.IP().String()
	}
	return v4gen.IP().String(), v4gen.IP().String()
}

func ports() (int, int) {
	var orig_port, resp_port int
	if rand.Int()%2 == 0 {
		orig_port = 1 + rand.Intn(2048)
		resp_port = 2048 + rand.Intn(0xffff-2048)
	} else {
		orig_port = 2048 + rand.Intn(0xffff-2048)
		resp_port = 1 + rand.Intn(2048)
	}
	return orig_port, resp_port
}

func randomBase62(l int) string {
	r := make([]byte, l)
	for i := 0; i < l; i++ {
		r[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(r)
}
