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
	"net"
	"strings"
	"time"

	rd "github.com/Pallinder/go-randomdata"
)

// argument order is: TS, <rand uint64> <clientip> <client port> <host> <host> <A or AAAAA> <serverip>
// TS format is
const format = `%v queries: info: client @0x%x %v#%d (%s): query: %s IN %s + (%v)`
const tsformat = `02-Jan-2006 15:04:05.999`

func genDataBind(ts time.Time) []byte {
	host, a := randHostname()
	var ip, server net.IP
	if a == `AAAA` {
		ip = v6gen.IP()
		server = serverIP6()
	} else {
		ip = v4gen.IP()
		server = serverIP()
	}
	return []byte(fmt.Sprintf(format, ts.Format(tsformat), randAddr(), ip, randPort(), host, host, a, server))
}

func randAddr() (r uint64) {
	r = rand.Uint64() & 0xfff
	r = r | 0x7f466d899000
	return
}

func randPort() (r uint16) {
	v := rand.Intn(0xdfff) + 0x2000
	r = uint16(v)
	return
}

var (
	queryTypes = []string{`A`, `AAAA`}
)

func randProto() string {
	if (rand.Uint32() & 0x7) == 0x7 {
		return queryTypes[1]
	}
	return queryTypes[0]
}

var (
	tlds    = []string{`io`, `com`, `net`, `us`, `co.uk`}
	badTLDs = []string{`gravwell`, `foobar`, `barbaz`}
)

func randTLD() string {
	return tlds[rand.Intn(len(tlds))]
}

func badTLD() string {
	return badTLDs[rand.Intn(len(badTLDs))]
}

func randHostname() (host, A string) {
	A = randProto()
	if r := rand.Uint32(); (r & 0x7) == 0x3 {
		host = randReverseLookupHost(A)
	} else if (r & 0x7f) == 42 {
		host = fmt.Sprintf("%s.%s", rd.Noun(), badTLD())
	} else {
		host = fmt.Sprintf("%s.%s.%s", rd.Noun(), rd.Noun(), randTLD())
	}
	return
}

func randReverseLookupHost(aaaa string) (host string) {
	if len(aaaa) == 4 {
		host = fmt.Sprintf("%s.ip6.arpa", ipRevGen(v6gen.IP()))
	} else {
		host = fmt.Sprintf("%s.in-addr.arpa", ipRevGen(v4gen.IP()))
	}
	return
}

func ipRevGen(ip net.IP) string {
	if len(ip) == 16 {
		return ip6RevGen(ip)
	}
	var sb strings.Builder
	end := len(ip) - 1
	for i := end; i >= 0; i-- {
		b := ip[i]
		if i == end {
			fmt.Fprintf(&sb, "%d", b)
		} else {
			fmt.Fprintf(&sb, ".%d", b)
		}
	}
	return sb.String()
}

func ip6RevGen(ip net.IP) string {
	var sb strings.Builder
	end := len(ip) - 1
	for i := end; i >= 0; i-- {
		b := ip[i]
		if i == end {
			fmt.Fprintf(&sb, "%x.%x", b&0xf, b>>4)
		} else {
			fmt.Fprintf(&sb, ".%x.%x", b&0xf, b>>4)
		}
	}
	return sb.String()
}

func serverIP() net.IP {
	return serverIPs[rand.Intn(len(serverIPs))]
}

func serverIP6() net.IP {
	return serverIP6s[rand.Intn(len(serverIP6s))]
}
