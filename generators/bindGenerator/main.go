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
	"strings"
	"time"

	rd "github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v3/generators/base"
	"github.com/gravwell/gravwell/v3/generators/ipgen"
	"github.com/gravwell/gravwell/v3/ingest"
)

var (
	v4gen     *ipgen.V4Gen
	v6gen     *ipgen.V6Gen
	serverIPs []net.IP
)

func init() {
	var err error
	v4gen, err = ipgen.RandomWeightedV4Generator(3)
	if err != nil {
		log.Fatalf("Failed to instantiate v4 generator: %v\n", err)
	}
	v6gen, err = ipgen.RandomWeightedV6Generator(30)
	if err != nil {
		log.Fatalf("Failed to instantiate v6 generator: %v\n", err)
	}
	for i := 0; i < 4; i++ {
		serverIPs = append(serverIPs, v4gen.IP())
	}
}

func main() {
	var genconn base.GeneratorConn
	var totalBytes uint64
	var totalCount uint64
	var src net.IP
	cfg, err := base.GetGeneratorConfig(`bind`)
	if err != nil {
		log.Fatal(err)
	}
	if genconn, src, err = base.NewIngestMuxer(`bindgenerator`, ``, cfg, time.Second); err != nil {
		log.Fatal(err)
	}
	tag, err := genconn.GetTag(cfg.Tag)
	if err != nil {
		log.Fatalf("Failed to lookup tag %s: %v", cfg.Tag, err)
	}
	start := time.Now()

	if !cfg.Streaming {
		if totalCount, totalBytes, err = base.OneShot(genconn, tag, src, cfg.Count, cfg.Duration, genData); err != nil {
			log.Fatal("Failed to throw entries ", err)
		}
	} else {
		if totalCount, totalBytes, err = base.Stream(genconn, tag, src, cfg.Count, genData); err != nil {
			log.Fatal("Failed to stream entries ", err)
		}
	}

	if err = genconn.Sync(time.Second); err != nil {
		log.Fatal("Failed to sync ingest muxer ", err)
	}

	if err = genconn.Close(); err != nil {
		log.Fatal("Failed to close ingest muxer ", err)
	}

	durr := time.Since(start)
	if err == nil {
		fmt.Printf("Completed in %v (%s)\n", durr, ingest.HumanSize(totalBytes))
		fmt.Printf("Total Count: %s\n", ingest.HumanCount(totalCount))
		fmt.Printf("Entry Rate: %s\n", ingest.HumanEntryRate(totalCount, durr))
		fmt.Printf("Ingest Rate: %s\n", ingest.HumanRate(totalBytes, durr))
	}
}

// argument order is: TS, <rand uint64> <clientip> <client port> <host> <host> <A or AAAAA> <serverip>
// TS format is
const format = `%v queries: info: client @0x%x %v#%d (%s): query: %s IN %s + (%v)`
const tsformat = `02-Jan-2006 15:04:05.999`

func genData(ts time.Time) []byte {
	host, a := randHostname()
	return []byte(fmt.Sprintf(format, ts.Format(tsformat), randAddr(), v4gen.IP(), randPort(), host, host, a, serverIP()))
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
	protos = []string{`A`, `AAAA`}
)

func randProto() string {
	if (rand.Uint32() & 0x7) == 0x7 {
		return protos[1]
	}
	return protos[0]
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
