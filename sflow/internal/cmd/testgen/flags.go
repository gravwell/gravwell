package main

import "flag"

var (
	port         int
	datagramSize int
	fixturesDir  string
	force        bool
)

func init() {
	flag.IntVar(&port, "port", 6343, "UDP port to listen on for sFlow packets")
	flag.IntVar(&datagramSize, "datagram-size", 2048, "Maximum datagram size in bytes")
	flag.StringVar(&fixturesDir, "fixtures-dir", "./fixtures", "Directory to save fixture files")
	flag.BoolVar(&force, "force", true, "override fixture file if already present")
}
