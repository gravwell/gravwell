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
)

func genDataZeekConn(ts time.Time) []byte {
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
