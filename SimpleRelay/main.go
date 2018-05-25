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
	"runtime/pprof"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/ingest/log"
	"github.com/gravwell/ingesters/version"
	"github.com/gravwell/timegrinder"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/simple_relay.conf`
	ingesterName     = `simplerelay`
	batchSize        = 512
)

var (
	cpuprofile     = flag.String("cpuprofile", "", "write cpu profile to file")
	configOverride = flag.String("config-file-override", "", "Override location for configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	ver            = flag.Bool("version", false, "Print the version information and exit")
	confLoc        string
	connClosers    map[int]closer
	connId         int
	mtx            sync.Mutex

	v  bool
	lg *log.Logger
)

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
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
		version.PrintVersion(fout)
		ingest.PrintVersion(fout)
	}
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing

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
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			lg.Fatal("Failed to open %s for profile file: %v\n", *cpuprofile, err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	cfg, err := GetConfig(confLoc)
	if err != nil {
		lg.FatalCode(0, "Failed to get configuration: %v\n", err)
		return
	}

	if len(cfg.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "Failed to open log file %s: %v", cfg.Log_File, err)
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("Failed to add a writer: %v", err)
		}
		if len(cfg.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Log_Level); err != nil {
				lg.FatalCode(0, "Invalid Log Level \"%s\": %v", cfg.Log_Level, err)
			}
		}
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.FatalCode(0, "Failed to get tags from configuration: %v\n", err)
		return
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.FatalCode(0, "Failed to get backend targets from configuration: %v\n", err)
		return
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	//fire up the ingesters
	debugout("INSECURE skip TLS certificate verification: %v\n", cfg.InsecureSkipTLSVerification())
	igCfg := ingest.UniformMuxerConfig{
		Destinations: conns,
		Tags:         tags,
		Auth:         cfg.Secret(),
		LogLevel:     cfg.LogLevel(),
		VerifyCert:   !cfg.InsecureSkipTLSVerification(),
		IngesterName: ingesterName,
	}
	if cfg.EnableCache() {
		igCfg.EnableCache = true
		igCfg.CacheConfig.FileBackingLocation = cfg.LocalFileCachePath()
		igCfg.CacheConfig.MaxCacheSize = cfg.MaxCachedData()
	}
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		lg.Fatal("Failed build our ingest system: %v\n", err)
		return
	}

	defer igst.Close()
	debugout("Started ingester muxer\n")
	if err := igst.Start(); err != nil {
		lg.Fatal("Failed start our ingest system: %v\n", err)
		return
	}
	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "Timedout waiting for backend connections: %v\n", err)
		return
	}
	debugout("Successfully connected to ingesters\n")
	wg := sync.WaitGroup{}
	ch := make(chan *entry.Entry, 2048)

	//fire up our backends
	for k, v := range cfg.Listener {
		var src net.IP
		if v.Source_Override != `` {
			src = net.ParseIP(v.Source_Override)
			if src == nil {
				lg.FatalCode(0, "Listener %v invalid source override, \"%s\" is not an IP address", k, v.Source_Override)
			}
		} else if cfg.Source_Override != `` {
			// global override
			src = net.ParseIP(cfg.Source_Override)
			if src == nil {
				lg.FatalCode(0, "Global Source-Override is invalid")
			}
		}
		//get the tag for this listener
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.Fatal("Failed to resolve tag \"%s\" for %s: %v\n", v.Tag_Name, k, err)
		}
		tp, str, err := translateBindType(v.Bind_String)
		if err != nil {
			lg.FatalCode(0, "Invalid bind string \"%s\": %v\n", v.Bind_String, err)
		}
		lrt, err := translateReaderType(v.Reader_Type)
		if err != nil {
			lg.FatalCode(0, "Invalid reader type \"%s\": %v\n", v.Reader_Type, err)
		}
		if tp.TCP() {
			//get the socket
			addr, err := net.ResolveTCPAddr(tp.String(), str)
			if err != nil {
				lg.FatalCode(0, "Bind-String \"%s\" for %s is invalid: %v\n", v.Bind_String, k, err)
			}
			l, err := net.ListenTCP(tp.String(), addr)
			if err != nil {
				lg.FatalCode(0, "Failed to listen on \"%s\" via %s for %s: %v\n", addr, tp.String(), k, err)
			}
			connID := addConn(l)
			//start the acceptor
			wg.Add(1)
			go acceptor(l, ch, tag, lrt, v.Ignore_Timestamps, v.Assume_Local_Timezone, v.Keep_Priority, &wg, connID, src, igst)
		} else if tp.UDP() {
			addr, err := net.ResolveUDPAddr(tp.String(), str)
			if err != nil {
				lg.FatalCode(0, "Bind-String \"%s\" for %s is invalid: %v\n", v.Bind_String, k, err)
			}
			l, err := net.ListenUDP(tp.String(), addr)
			if err != nil {
				lg.FatalCode(0, "Failed to listen on \"%s\" via %s for %s: %v\n", addr, tp.String(), k, err)
			}
			connID := addConn(l)
			wg.Add(1)
			go acceptorUDP(l, ch, tag, lrt, v.Ignore_Timestamps, v.Assume_Local_Timezone, v.Keep_Priority, &wg, connID, src)
		}

	}
	debugout("Started %d listeners\n", len(cfg.Listener))
	//fire off our relay
	doneChan := make(chan bool)
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
	case <-time.After(1 * time.Second):
		lg.Error("Failed to wait for all connections to close.  %d active\n", connCount())
	}
	if err := igst.Sync(time.Second); err != nil {
		lg.Error("Failed to sync: %v\n", err)
	}
	if err := igst.Close(); err != nil {
		lg.Error("Failed to close: %v\n", err)
	}
}

func relay(ch chan *entry.Entry, done chan bool, igst *ingest.IngestMuxer) {
	var ents []*entry.Entry

	tckr := time.NewTicker(time.Second)
	defer tckr.Stop()
mainLoop:
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				if len(ents) > 0 {
					if err := igst.WriteBatch(ents); err != nil {
						if err != ingest.ErrNotRunning {
							fmt.Fprintf(os.Stderr, "Failed to throw batch: %v\n", err)
						}
					}
				}
				ents = nil
				break mainLoop
			}
			if e != nil {
				ents = append(ents, e)
			}
			if len(ents) >= batchSize {
				if err := igst.WriteBatch(ents); err != nil {
					if err != ingest.ErrNotRunning {
						fmt.Fprintf(os.Stderr, "Failed to throw batch: %v\n", err)
					} else {
						break mainLoop
					}
				}
				ents = nil
			}
		case _ = <-tckr.C:
			if len(ents) > 0 {
				if err := igst.WriteBatch(ents); err != nil {
					if err != ingest.ErrNotRunning {
						fmt.Fprintf(os.Stderr, "Failed to throw batch: %v\n", err)
					} else {
						break mainLoop
					}
				}
				ents = nil
			}
		}
	}
	close(done)
}

func acceptor(lst net.Listener, ch chan *entry.Entry, tag entry.EntryTag, lrt readerType, ignoreTimestamps, setLocalTime, keepPriority bool, wg *sync.WaitGroup, id int, src net.IP, igst *ingest.IngestMuxer) {
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
		igst.Info("accepted TCP connection from %s in %v mode\n", conn.RemoteAddr(), lrt)
		failCount = 0
		switch lrt {
		case lineReader:
			go lineConnHandlerTCP(conn, ch, ignoreTimestamps, setLocalTime, tag, wg, src)
		case rfc5424Reader:
			go rfc5424ConnHandlerTCP(conn, ch, ignoreTimestamps, setLocalTime, !keepPriority, tag, wg, src)
		default:
			fmt.Fprintf(os.Stderr, "Invalid reader type on connection\n")
			return
		}
	}
}

func acceptorUDP(conn *net.UDPConn, ch chan *entry.Entry, tag entry.EntryTag, lrt readerType, ignoreTimestamps, setLocalTime, keepPriority bool, wg *sync.WaitGroup, id int, src net.IP) {
	defer wg.Done()
	defer delConn(id)
	defer conn.Close()
	//read packets off
	switch lrt {
	case lineReader:
		lineConnHandlerUDP(conn, ch, ignoreTimestamps, setLocalTime, tag, wg, src)
	case rfc5424Reader:
		rfc5424ConnHandlerUDP(conn, ch, ignoreTimestamps, setLocalTime, !keepPriority, tag, wg, src)
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
	//debugout("GOT (%v) %s\n", ts, string(b))
	ch <- &entry.Entry{
		SRC:  ip,
		TS:   ts,
		Tag:  tag,
		Data: b,
	}
	return nil
}

func debugout(format string, args ...interface{}) {
	if !v {
		return
	}
	fmt.Printf(format, args...)
}
