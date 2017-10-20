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
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/timegrinder"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/simple_relay.conf`
	chunkSize        = 1024 * 1024 * 4
)

var (
	configOverride = flag.String("config-file-override", "", "Override location for configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	confLoc        string
	connClosers    map[int]closer
	connId         int
	mtx            sync.Mutex

	v bool
)

func init() {
	flag.Parse()

	if *stderrOverride != `` {
		fp := path.Join(`/dev/shm/`, *stderrOverride)
		fout, err := os.Create(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", fp, err)
		} else {
			//file created, dup it
			if err := syscall.Dup2(int(fout.Fd()), int(os.Stderr.Fd())); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to dup2 stderr: %v\n", err)
				fout.Close()
			}
		}
	}

	if *configOverride == "" {
		confLoc = defaultConfigLoc
	} else {
		confLoc = *configOverride
	}
	v = *verbose
	connClosers = make(map[int]closer, 1)
}

type closer interface {
	Close() error
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

func connCount() int {
	mtx.Lock()
	defer mtx.Unlock()
	return len(connClosers)
}

func main() {
	cfg, err := GetConfig(confLoc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get configuration: %v\n", err)
		return
	}

	tags, err := cfg.Tags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get tags from configuration: %v\n", err)
		return
	}
	conns, err := cfg.Targets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get backend targets from configuration: %v\n", err)
		return
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	//fire up the ingesters
	igCfg := ingest.UniformMuxerConfig{
		Destinations: conns,
		Tags:         tags,
		Auth:         cfg.Secret(),
		LogLevel:     cfg.LogLevel(),
		VerifyCert:   !cfg.VerifyRemote(),
	}
	if cfg.EnableCache() {
		igCfg.EnableCache = true
		igCfg.CacheConfig.FileBackingLocation = cfg.LocalFileCachePath()
	}
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed build our ingest system: %v\n", err)
		return
	}

	defer igst.Close()
	debugout("Started ingester muxer\n")
	if err := igst.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed start our ingest system: %v\n", err)
		return
	}
	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		fmt.Fprintf(os.Stderr, "Timedout waiting for backend connections: %v\n", err)
		return
	}
	debugout("Successfully connected to ingesters\n")
	wg := sync.WaitGroup{}
	ch := make(chan *entry.Entry, 2048)

	//fire up our backends
	for k, v := range cfg.Listener {
		//get the tag for this listener
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to resolve tag \"%s\" for %s: %v\n", v.Tag_Name, k, err)
			return
		}
		tp, str, err := translateBindType(v.Bind_String)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid bind string \"%s\": %v\n", v.Bind_String, err)
			return
		}
		lrt, err := translateReaderType(v.Reader_Type)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid reader type \"%s\": %v\n", v.Reader_Type, err)
			return
		}
		if tp.TCP() {
			//get the socket
			addr, err := net.ResolveTCPAddr(tp.String(), str)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Bind-String \"%s\" for %s is invalid: %v\n", v.Bind_String, k, err)
				return
			}
			l, err := net.ListenTCP(tp.String(), addr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to listen on \"%s\" via %s for %s: %v\n", addr, tp.String(), k, err)
				return
			}
			connID := addConn(l)
			//start the acceptor
			wg.Add(1)
			go acceptor(l, ch, tag, lrt, v.Ignore_Timestamps, v.Assume_Local_Timezone, &wg, connID)
		} else if tp.UDP() {
			addr, err := net.ResolveUDPAddr(tp.String(), str)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Bind-String \"%s\" for %s is invalid: %v\n", v.Bind_String, k, err)
				return

			}
			l, err := net.ListenUDP(tp.String(), addr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to listen on \"%s\" via %s for %s: %v\n", addr, tp.String(), k, err)
				return
			}
			connID := addConn(l)
			wg.Add(1)
			go acceptorUDP(l, ch, tag, lrt, v.Ignore_Timestamps, v.Assume_Local_Timezone, &wg, connID)
		}

	}
	debugout("Started %d listeners\n", len(cfg.Listener))
	//fire off our relay
	doneChan := make(chan error, 1)
	go relay(ch, doneChan, igst)

	debugout("Running\n")

	//listen for signals so we can close gracefully
	sch := make(chan os.Signal, 1)
	signal.Notify(sch, os.Interrupt)
	<-sch
	debugout("Closing %d connections\n", connCount())
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
		close(ch)
		//wait for our ingest relay to exit
		<-doneChan
	case <-time.After(time.Second):
		fmt.Fprintf(os.Stderr, "Failed to wait for all connections to close.  %d active\n", connCount())
	}

	if err := igst.StopAndSync(time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to sync: %v\n", err)
	}
	if err := igst.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to close: %v\n", err)
	}
}

func throwBatch(igst *ingest.IngestMuxer, ents []*entry.Entry) error {
	if len(ents) == 0 {
		return nil
	} else if len(ents) == 1 {
		return igst.WriteEntry(ents[0])
	}
	return igst.WriteBatch(ents)
}

func relay(ch chan *entry.Entry, done chan error, igst *ingest.IngestMuxer) {
	var ents []*entry.Entry
	var lastTS int64
	var currSize uint64

	//grab the first entry
	for {
		e, ok := <-ch
		if !ok {
			done <- nil
			return
		}
		if e == nil {
			continue
		}
		lastTS = e.TS.Sec
		ents = append(ents, e)
		currSize += e.Size()
		break
	}

	tckr := time.NewTicker(time.Second)

mainLoop:
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				if err := throwBatch(igst, ents); err != nil {
					if err != ingest.ErrNotRunning {
						fmt.Fprintf(os.Stderr, "Failed to throw batch: %v\n", err)
					}
				}
				ents = nil
				currSize = 0
				break mainLoop
			}
			if e != nil {
				if lastTS != e.TS.Sec {
					if err := throwBatch(igst, ents); err != nil {
						if err != ingest.ErrNotRunning {
							fmt.Fprintf(os.Stderr, "Failed to throw batch: %v\n", err)
						} else {
							break mainLoop
						}
					}
					ents = nil
					currSize = 0
				}
				ents = append(ents, e)
				currSize += e.Size()
			}
		case _ = <-tckr.C:
			if len(ents) > 0 {
				if err := throwBatch(igst, ents); err != nil {
					if err != ingest.ErrNotRunning {
						fmt.Fprintf(os.Stderr, "Failed to throw batch: %v\n", err)
					} else {
						break mainLoop
					}
				}
				ents = nil
				currSize = 0
			}
		}
	}
	done <- nil
}

func acceptor(lst net.Listener, ch chan *entry.Entry, tag entry.EntryTag, lrt readerType, ignoreTimestamps, setLocalTime bool, wg *sync.WaitGroup, id int) {
	var failCount int
	defer wg.Done()
	defer delConn(id)
	defer lst.Close()
	for {
		conn, err := lst.Accept()
		if err != nil {
			if strings.Contains(err.Error(), "closed") {
				break
			}
			failCount++
			fmt.Fprintf(os.Stderr, "Failed to accept TCP connection: %v\n", err)
			if failCount > 3 {
				break
			}
			continue
		}
		debugout("Accepted TCP connection from %s in %v mode\n", conn.RemoteAddr(), lrt)
		failCount = 0
		switch lrt {
		case lineReader:
			go lineConnHandlerTCP(conn, ch, ignoreTimestamps, setLocalTime, tag, wg)
		case rfc5424Reader:
			go rfc5424ConnHandlerTCP(conn, ch, ignoreTimestamps, setLocalTime, tag, wg)
		default:
			fmt.Fprintf(os.Stderr, "Invalid reader type on connection\n")
			return
		}
	}
}

func acceptorUDP(conn *net.UDPConn, ch chan *entry.Entry, tag entry.EntryTag, lrt readerType, ignoreTimestamps, setLocalTime bool, wg *sync.WaitGroup, id int) {
	defer wg.Done()
	defer delConn(id)
	defer conn.Close()
	//read packets off
	switch lrt {
	case lineReader:
		lineConnHandlerUDP(conn, ch, ignoreTimestamps, setLocalTime, tag, wg)
	case rfc5424Reader:
		rfc5424ConnHandlerUDP(conn, ch, ignoreTimestamps, setLocalTime, true, tag, wg)
	default:
		fmt.Fprintf(os.Stderr, "Invalid reader type on connection\n")
		return
	}
}

func handleLog(b []byte, ip net.IP, ignoreTS bool, tag entry.EntryTag, ch chan *entry.Entry, tg *timegrinder.TimeGrinder) error {
	if len(b) == 0 {
		return nil
	}
	var ok bool
	var ts entry.Timestamp
	var extracted time.Time
	var err error
	if !ignoreTS {
		extracted, ok, err = tg.Extract(b)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Catastrophic timegrinder failure: %v\n", err)
			return err
		}
		ts = entry.FromStandard(extracted)
	}
	if !ok {
		ts = entry.Now()
	}
	debugout("GOT (%v) %s\n", ts, string(b))
	ch <- &entry.Entry{
		SRC:  ip,
		TS:   ts,
		Tag:  tag,
		Data: append([]byte(nil), b...),
	}
	return nil
}

func debugout(format string, args ...interface{}) {
	if !v {
		return
	}
	fmt.Printf(format, args...)
}
