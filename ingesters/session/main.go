/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	gravwelldebug "github.com/gravwell/gravwell/v4/debug"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
	"github.com/gravwell/gravwell/v4/ingesters/version"
)

var (
	bindString    = flag.String("bind", "0.0.0.0:7777", "Bind string specifying optional IP and port to listen on")
	maxMBSize     = flag.Int("max-session-mb", 8, "Maximum MBs a single session will accept")
	tagName       = flag.String("tag-name", entry.DefaultTagName, "Tag name for ingested data")
	clearConns    = flag.String("clear-conns", "", "Comma-separated server:port list of cleartext targets")
	tlsConns      = flag.String("tls-conns", "", "Comma-separated server:port list of TLS connections")
	pipeConns     = flag.String("pipe-conns", "", "Comma-separated list of paths for named pipe connection")
	tlsPublicKey  = flag.String("tls-public-key", "", "Path to TLS public key")
	tlsPrivateKey = flag.String("tls-private-key", "", "Path to TLS private key")
	tlsNoVerify   = flag.Bool("insecure-tls-remote-noverify", false, "Do not validate remote TLS certs")
	ingestSecret  = flag.String("ingest-secret", "IngestSecrets", "Ingest key")
	timeoutSec    = flag.Int("timeout", 1, "Connection timeout in seconds")
	ver           = flag.Bool("version", false, "Print the version information and exit")
	connSet       []string
	timeout       time.Duration

	connClosers map[int]closer
	connId      int
	mtx         sync.Mutex
	maxSize     int
)

type closer interface {
	Close() error
}

type ingestUnit struct {
	igst *ingest.IngestMuxer
	tag  entry.EntryTag
}

type results struct {
	Bytes uint64
	Count uint64
	Error error
}

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	if *timeoutSec <= 0 {
		fmt.Printf("Invalid timeout\n")
		os.Exit(-1)
	}
	timeout = time.Second * time.Duration(*timeoutSec)

	if *maxMBSize <= 0 || *maxMBSize > 1024 {
		fmt.Printf("Invalid max-session-mb, must be > 0 < 1024\n")
		os.Exit(-1)
	}
	maxSize = *maxMBSize * 1024 * 1024

	if *ingestSecret == `` {
		fmt.Printf("No ingest secret specified\n")
		os.Exit(-1)
	}

	if *tagName == "" {
		fmt.Printf("A tag name must be specified\n")
		os.Exit(-1)
	} else {
		//verify that the tag name is valid
		*tagName = strings.TrimSpace(*tagName)
		if ingest.CheckTag(*tagName) != nil {
			fmt.Printf("Forbidden characters in tag\n")
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
	connClosers = make(map[int]closer, 1)
}

func main() {
	go gravwelldebug.HandleDebugSignals("session")
	debug.SetTraceback("all")
	cfg := ingest.UniformMuxerConfig{
		Destinations:    connSet,
		Tags:            []string{*tagName},
		Auth:            *ingestSecret,
		PublicKey:       *tlsPublicKey,
		PrivateKey:      *tlsPrivateKey,
		IngesterVersion: version.GetVersion(),
		IngesterName:    `session`,
		VerifyCert:      *tlsNoVerify,
	}
	igst, err := ingest.NewUniformMuxer(cfg)
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
	//get the TagID for our default tag
	tag, err := igst.GetTag(*tagName)
	if err != nil {
		fmt.Printf("Failed to look up tag %s: %v\n", *tagName, err)
		os.Exit(-1)
	}

	l, err := net.Listen("tcp", *bindString)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to bind to %s: %v\n", *bindString, err)
		os.Exit(-1)
	}
	entChan := make(chan *entry.Entry, 8)
	doneChan := make(chan bool, 1)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go acceptor(l, entChan, tag, doneChan, &wg)

	//listen for signals so we can close gracefully
	sch := make(chan os.Signal, 1)
	signal.Notify(sch, os.Interrupt, syscall.SIGTERM)

mainLoop:
	for {
		select {
		case <-sch:
			break mainLoop
		case e := <-entChan:
			if err := igst.WriteEntry(e); err != nil {
				fmt.Println("failed to write entry", err)
				break mainLoop
			}
			fmt.Println("Sent over a", len(e.Data), "package")
		}
	}
	//kill connections
	mtx.Lock()
	for _, v := range connClosers {
		v.Close()
	}
	mtx.Unlock() //must unlock so they can delete their connections

	//wait for everyone to exit with a timeout
	wch := make(chan bool, 1)

	go func() {
		wg.Wait()
		wch <- true
	}()
	select {
	case <-wch:
		//close our output channel
		close(entChan)
	case <-time.After(time.Second):
		fmt.Fprintf(os.Stderr, "Failed to wait for all connections to close\n")
	}
	//wait for our ingest relay to exit
	<-doneChan
	if err := igst.Sync(utils.ExitSyncTimeout); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to sync: %v\n", err)
	}
	igst.Close()
}

func addConn(c closer) int {
	mtx.Lock()
	connId++
	id := connId
	connClosers[connId] = c
	mtx.Unlock()
	return id
}

func delConn(id int) {
	mtx.Lock()
	delete(connClosers, id)
	mtx.Unlock()
}

func acceptor(l net.Listener, entChan chan *entry.Entry, tag entry.EntryTag, doneChan chan bool, wg *sync.WaitGroup) {
	id := addConn(l)
	defer l.Close()
	defer delConn(id)
	defer wg.Done()
	for {
		c, err := l.Accept()
		if err != nil {
			break
		}
		wg.Add(1)
		go connHandler(c, entChan, tag, wg)
	}
	doneChan <- true
}

func connHandler(c net.Conn, entChan chan *entry.Entry, tag entry.EntryTag, wg *sync.WaitGroup) {
	id := addConn(c)
	defer c.Close()
	defer delConn(id)
	defer wg.Done()

	//attempt to resolve the remote connection
	ipS, _, err := net.SplitHostPort(c.RemoteAddr().String())
	if err != nil {
		fmt.Println("Failed to split IP from port on", c.RemoteAddr().String(), ":", err)
		return
	}
	src := net.ParseIP(ipS)
	if src == nil {
		fmt.Println("Failed to parse IP from", ipS)
		return
	}

	tbuff := make([]byte, 512*1024)
	bb := bytes.NewBuffer(nil)
	ts := entry.Now()
	c.SetReadDeadline(time.Now().Add(10 * time.Second)) //connection has 10 seconds to do its business
	for {
		n, err := c.Read(tbuff)
		if err != nil {
			if err == io.EOF {
				if n > 0 {
					bb.Write(tbuff[:n])
				}
				break
			}
			return
		}
		bb.Write(tbuff[:n])
	}
	if bb.Len() > 0 {
		e := &entry.Entry{
			TS:   ts,
			SRC:  src,
			Tag:  tag,
			Data: bb.Bytes(),
		}
		entChan <- e
	}
}
