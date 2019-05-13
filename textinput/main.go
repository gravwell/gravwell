/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
package main

import (
	"bufio"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"strings"

	"github.com/gravwell/ipexist"
)

var (
	fIn  = flag.String("i", "", "Input file")
	fOut = flag.String("o", "output.ipe", "Output file")
)

func init() {
	flag.Parse()
	if *fIn == `` {
		flag.PrintDefaults()
		log.Fatalf("Missing input file, specify something for -i\n")
	} else if *fOut == `` {
		flag.PrintDefaults()
		log.Fatalf("Missing output file, specify something for -o\n")
	} else if *fIn == *fOut {
		log.Fatalf("Input and Output files cannot be the same\n")
	}
}

func main() {
	fin, err := os.Open(*fIn)
	if err != nil {
		log.Fatalf("Failed to open %s: %v\n", *fIn, err)
	}
	defer fin.Close()
	fout, err := os.Create(*fOut)
	if err != nil {
		log.Fatalf("Failed to create %s: %v\n", *fOut, err)
	}
	defer fout.Close()

	ipb := ipexist.NewIPBitMap()

	r := bufio.NewReader(fin)
	var cnt int
	for {
		s, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			break
		}
		s = strings.TrimSpace(strings.Trim(s, "\n\r\"'"))
		if ip := net.ParseIP(s); ip != nil {
			if ip = ip.To4(); ip != nil {
				if err := ipb.AddIP(ip); err != nil {
					log.Fatalf("Failed to add %s: %v\n", ip, err)
				}
				cnt++
			}
		}
	}
	if err = ipb.Encode(fout); err != nil {
		log.Fatalf("Failied to encode output file: %v\n", err)
	}
	log.Printf("Processed %d IPs\n", cnt)
}
