/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingesters/version"
	"github.com/shirou/gopsutil/mem"
)

var (
	tagName         = flag.String("tag-name", entry.DefaultTagName, "Tag name for ingested data")
	pipeConns       = flag.String("pipe-conn", "", "Path to pipe connection")
	clearConns      = flag.String("clear-conns", "", "Comma-separated server:port list of cleartext targets")
	tlsConns        = flag.String("tls-conns", "", "Comma-separated server:port list of TLS connections")
	tlsPublicKey    = flag.String("tls-public-key", "", "Path to TLS public key")
	tlsPrivateKey   = flag.String("tls-private-key", "", "Path to TLS private key")
	tlsRemoteVerify = flag.String("tls-remote-verify", "", "Path to remote public key to verify against")
	ingestSecret    = flag.String("ingest-secret", "IngestSecrets", "Ingest key")
	timeoutSec      = flag.Int("timeout", 1, "Connection timeout in seconds")
	tzo             = flag.String("timezone-override", "", "Timezone override e.g. America/Chicago")

	srcDir   = flag.String("s", "", "Source directory containing log files")
	wDir     = flag.String("w", "", "Working directory for optimization")
	noIngest = flag.Bool("no-ingest", false, "Optimize logs but do not perform ingest")
	skipOp   = flag.Bool("skip-op", false, "Assume working directory already has optimized logs")
	ver      = flag.Bool("version", false, "Print the version information and exit")
	connSet  []string
	timeout  time.Duration
	working  string
	source   string
)

type ingestVars struct {
	m   *ingest.IngestMuxer
	tag entry.EntryTag
	src net.IP
}

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	timeout = time.Second * time.Duration(*timeoutSec)
	if *srcDir == "" && !*skipOp {
		fmt.Fprintf(os.Stderr, "A source directory -s is required\n")
		os.Exit(-1)
	}
	if *wDir == "" {
		fmt.Fprintf(os.Stderr, "A working directory -w is required\n")
		os.Exit(-1)
	}
	if !*skipOp {
		//if we aren't skipping optimization, then check dir
		fi, err := os.Stat(*srcDir)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "Source directory %s does not exist\n", *srcDir)
				os.Exit(-1)
			}
		}
		if !fi.IsDir() {
			fmt.Fprintf(os.Stderr, "%s is not a directory\n", *srcDir)
			os.Exit(-1)
		}
	}
	fi, err := os.Stat(*wDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "%s does not exist\n", *wDir)
			os.Exit(-1)
		}
	}
	if !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "%s is not a directory\n", *wDir)
		os.Exit(-1)
	}
	working = *wDir
	source = *srcDir
	if *tagName == "" {
		fmt.Printf("A tag name must be specified\n")
		os.Exit(-1)
	} else {
		//verify that the tag name is valid
		*tagName = strings.TrimSpace(*tagName)
		if err := ingest.CheckTag(*tagName); err != nil {
			fmt.Printf("Forbidden characters in tag: %v\n", err)
			os.Exit(-1)
		}
	}
	if *clearConns != "" {
		for _, conn := range strings.Split(*clearConns, ",") {
			conn = strings.TrimSpace(conn)
			if len(conn) > 0 {
				connSet = append(connSet, fmt.Sprintf("tcp://%s", conn))
			}
		}
	}
	if *tlsConns != "" {
		if *tlsPublicKey == "" || *tlsPrivateKey == "" {
			fmt.Printf("Public/private keys required for TLS connection\n")
			os.Exit(-1)
		}
		for _, conn := range strings.Split(*tlsConns, ",") {
			conn = strings.TrimSpace(conn)
			if len(conn) > 0 {
				connSet = append(connSet, fmt.Sprintf("tls://%s", conn))
			}
		}
	}
	if *pipeConns != "" {
		for _, conn := range strings.Split(*pipeConns, ",") {
			conn = strings.TrimSpace(conn)
			if len(conn) > 0 {
				connSet = append(connSet, fmt.Sprintf("pipe://%s", conn))
			}
		}
	}
	if len(connSet) <= 0 {
		fmt.Printf("No connections were specified\nWe need at least one\n")
		os.Exit(-1)
	}
}

func main() {
	debug.SetTraceback("all")
	var sz int64
	var cnt int
	if !*skipOp {
		//size is determined by unoptimized set
		var err error
		sz, cnt, err = getLogSetSize(source)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get unoptimized log set size: %v\n", err)
			return
		}
		fmt.Printf("Preparing to process %s of logs across %d files\n", ingest.HumanSize(uint64(sz)), cnt)
		estimatedPerFileSize := sz / maxFileHandles
		memstats, err := mem.VirtualMemory()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get free ram: %v\n", err)
			os.Exit(-1)
		}
		if uint64(estimatedPerFileSize) > memstats.Available {
			fmt.Printf("WARNING: We estimate individual working sets to be about %s\n", ingest.HumanSize(uint64(estimatedPerFileSize)))
			fmt.Printf("\tBut your system only has %s available memory\n", ingest.HumanSize(uint64(memstats.Available)))
			fmt.Printf("\tWe will more than likely push into swap and slow way down\n")
		}
		if err := groupLargeLogs(source, working, sz); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to segment the logs: %v\n", err)
			return
		}
	} else {
		var err error
		sz, cnt, err = getLogSetSize(working)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get working log set size: %v\n", err)
			return
		}
		fmt.Printf("Preparing to ingest %s of logs\n", ingest.HumanSize(uint64(sz)))

	}
	var iv *ingestVars
	if !*noIngest {
		fmt.Println("Attaching to gravwell backends...")
		igst, err := ingest.NewUniformIngestMuxer(connSet, []string{*tagName}, *ingestSecret, "", "", "")
		if err != nil {
			fmt.Printf("ERROR: %v\n", err)
			os.Exit(-1)
		}
		if err := igst.Start(); err != nil {
			fmt.Printf("ERROR: failed to start ingester: %v\n", err)
			os.Exit(-2)
		}
		if err := igst.WaitForHot(timeout); err != nil {
			fmt.Printf("ERROR: Timed out waiting for active connection\n")
			os.Exit(-3)
		}
		tag, err := igst.GetTag(*tagName)
		if err != nil {
			fmt.Printf("ERROR: Failed to resolve tag %s: %v\n", *tagName, err)
			os.Exit(-4)
		}
		src, err := igst.SourceIP()
		if err != nil {
			fmt.Printf("ERROR: Failed to get source IP for ingesting: %v\n", err)
			os.Exit(-5)
		}
		iv = &ingestVars{
			m:   igst,
			tag: tag,
			src: src,
		}
		fmt.Println("Attached")
	}
	if !*skipOp {
		if err := optimizeGroups(working, sz, iv); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to optimized the segments: %v\n", err)
			os.Exit(-1)
		}
	} else if iv != nil {
		if err := ingestFromFiles(working, sz, iv); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to ingest pre-optimized files: %v\n", err)
			os.Exit(-1)
		}
	}
	if iv != nil {
		if err := iv.m.Sync(time.Second); err != nil {
			fmt.Printf("ERROR: Failed to sync ingester: %v\n", err)
			os.Exit(-1)
		}
		if err := iv.m.Close(); err != nil {
			fmt.Printf("ERROR: Failed to close ingester: %v\n", err)
			os.Exit(-1)
		}
	}
}

func getLogSetSize(srcDir string) (int64, int, error) {
	var totalSize int64
	var total int
	if err := filepath.Walk(srcDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		totalSize += fi.Size()
		total++
		return nil
	}); err != nil {
		return -1, -1, err
	}
	return totalSize, total, nil
}
