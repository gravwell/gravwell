package main

import "flag"

var (
	port         int
	datagramSize int
	testsDir     string
	force        bool
)

func init() {
	flag.IntVar(&port, "port", 6343, "UDP port to listen on for sFlow packets")
	flag.IntVar(&datagramSize, "datagram-size", 2048, "Maximum datagram size in bytes")
	flag.StringVar(&testsDir, "tests-dir", "./tests", "Directory to save test files")
	flag.BoolVar(&force, "force", true, "override test file if already present")
}
