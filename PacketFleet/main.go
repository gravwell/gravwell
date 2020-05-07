/*************************************************************************
 * Copyright 2020 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"sync"
	"time"

	"github.com/gravwell/ingest/v3"
	"github.com/gravwell/ingest/v3/config"
	"github.com/gravwell/ingest/v3/entry"
	"github.com/gravwell/ingest/v3/log"
	"github.com/gravwell/ingest/v3/processors"
	"github.com/gravwell/ingesters/v3/utils"
	"github.com/gravwell/ingesters/v3/version"

	pcap "github.com/google/gopacket/pcapgo"
)

const (
	defaultConfigLoc     = `/opt/gravwell/etc/packet_fleet.conf`
	ingesterName         = `PacketFleet`
	batchSize            = 512
	maxDataSize      int = 8 * 1024 * 1024
	initDataSize     int = 512 * 1024
)

var (
	cpuprofile     = flag.String("cpuprofile", "", "write cpu profile to file")
	confLoc        = flag.String("config-file", defaultConfigLoc, "Location for configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	ver            = flag.Bool("version", false, "Print the version information and exit")

	v    bool
	lg   *log.Logger
	igst *ingest.IngestMuxer
)

type handlerConfig struct {
	url              string
	caCert           string
	clientCert       string
	clientKey        string
	tag              entry.EntryTag
	ignoreTimestamps bool
	setLocalTime     bool
	timezoneOverride string
	src              net.IP
	formatOverride   string
	wg               *sync.WaitGroup
	proc             *processors.ProcessorSet

	client *http.Client
}

var (
	jobs    []*job
	jcount  uint
	jobLock sync.Mutex
	stenos  map[string]*handlerConfig
)

type poster struct {
	Q string   // query
	S string   // source override
	C []string // connections to submit query to
}

type server struct{}

type job struct {
	ID     uint
	Bytes  uint
	Query  string
	Source uint32
	Conns  []string
	lock   sync.Mutex
}

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	var fp string
	var err error
	if *stderrOverride != `` {
		fp = filepath.Join(`/dev/shm/`, *stderrOverride)
	}
	cb := func(w io.Writer) {
		version.PrintVersion(w)
		ingest.PrintVersion(w)
	}
	if lg, err = log.NewStderrLoggerEx(fp, cb); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get stderr logger: %v\n", err)
		os.Exit(-1)
	}

	v = *verbose
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

	cfg, err := GetConfig(*confLoc)
	if err != nil {
		var tcfg cfgType
		fmt.Printf("%+v\n", tcfg)
		lg.FatalCode(0, "Failed to get configuration: %v\n", err)
		return
	}

	if len(cfg.Global.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Global.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "Failed to open log file %s: %v", cfg.Global.Log_File, err)
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("Failed to add a writer: %v", err)
		}
		if len(cfg.Global.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Global.Log_Level); err != nil {
				lg.FatalCode(0, "Invalid Log Level \"%s\": %v", cfg.Global.Log_Level, err)
			}
		}
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.FatalCode(0, "Failed to get tags from configuration: %v\n", err)
		return
	}
	conns, err := cfg.Global.Targets()
	if err != nil {
		lg.FatalCode(0, "Failed to get backend targets from configuration: %v\n", err)
		return
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	lmt, err := cfg.Global.RateLimit()
	if err != nil {
		lg.FatalCode(0, "Failed to get rate limit from configuration: %v\n", err)
		return
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	//fire up the ingesters
	debugout("INSECURE skip TLS certificate verification: %v\n", cfg.Global.InsecureSkipTLSVerification())
	id, ok := cfg.Global.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID\n")
	}
	igCfg := ingest.UniformMuxerConfig{
		Destinations:    conns,
		Tags:            tags,
		Auth:            cfg.Global.Secret(),
		LogLevel:        cfg.Global.LogLevel(),
		VerifyCert:      !cfg.Global.InsecureSkipTLSVerification(),
		IngesterName:    ingesterName,
		IngesterVersion: version.GetVersion(),
		IngesterUUID:    id.String(),
		RateLimitBps:    lmt,
		Logger:          lg,
	}
	if cfg.Global.EnableCache() {
		igCfg.EnableCache = true
		igCfg.CacheConfig.FileBackingLocation = cfg.Global.LocalFileCachePath()
		igCfg.CacheConfig.MaxCacheSize = cfg.Global.MaxCachedData()
	}
	igst, err = ingest.NewUniformMuxer(igCfg)
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
	if err := igst.WaitForHot(cfg.Global.Timeout()); err != nil {
		lg.FatalCode(0, "Timedout waiting for backend connections: %v\n", err)
		return
	}
	debugout("Successfully connected to ingesters\n")
	var wg sync.WaitGroup

	// setup stenographer connections
	stenos = make(map[string]*handlerConfig)

	for k, v := range cfg.Stenographer {
		var src net.IP

		if v.Source_Override != `` {
			src = net.ParseIP(v.Source_Override)
			if src == nil {
				lg.FatalCode(0, "Listener %v invalid source override, \"%s\" is not an IP address", k, v.Source_Override)
			}
		} else if cfg.Global.Source_Override != `` {
			// global override
			src = net.ParseIP(cfg.Global.Source_Override)
			if src == nil {
				lg.FatalCode(0, "Global Source-Override is invalid")
			}
		}

		//get the tag for this listener
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.Fatal("Failed to resolve tag \"%s\" for %s: %v\n", v.Tag_Name, k, err)
		}

		hcfg := &handlerConfig{
			url:              v.URL,
			caCert:           v.CA_Cert,
			clientCert:       v.Client_Cert,
			clientKey:        v.Client_Key,
			tag:              tag,
			ignoreTimestamps: v.Ignore_Timestamps,
			setLocalTime:     v.Assume_Local_Timezone,
			timezoneOverride: v.Timezone_Override,
			formatOverride:   v.Timestamp_Format_Override,
			src:              src,
			wg:               &wg,
		}

		if hcfg.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Fatal("Preprocessor failure: %v", err)
		}

		// Load client cert
		cert, err := tls.LoadX509KeyPair(hcfg.clientCert, hcfg.clientKey)
		if err != nil {
			lg.Error("%v", err)
			return
		}

		// Load CA cert
		caCert, err := ioutil.ReadFile(hcfg.caCert)
		if err != nil {
			lg.Error("%v", err)
			return
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)

		// Setup HTTPS client
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true,
			Certificates:       []tls.Certificate{cert},
			RootCAs:            caCertPool,
		}
		tlsConfig.BuildNameToCertificate()
		transport := &http.Transport{TLSClientConfig: tlsConfig}
		hcfg.client = &http.Client{Transport: transport}

		stenos[k] = hcfg
	}

	s := &server{}
	go s.listener(cfg.Global.Listen_Address, cfg.Global.Server_Cert, cfg.Global.Server_Key)

	debugout("Running\n")

	//listen for signals so we can close gracefully
	utils.WaitForQuit()

	if err := igst.Sync(time.Second); err != nil {
		lg.Error("Failed to sync: %v\n", err)
	}
	if err := igst.Close(); err != nil {
		lg.Error("Failed to close: %v\n", err)
	}
}

func debugout(format string, args ...interface{}) {
	if !v {
		return
	}
	fmt.Printf(format, args...)
}

func (s *server) listener(laddr, c, k string) {
	// start our listener
	srv := &http.Server{
		Addr:    laddr,
		Handler: s,
	}
	lg.Error("%v", srv.ListenAndServeTLS(c, k))
}

// handler is the mux for all stenographer connections, and provides the
// following HTTP interface:
//	GET /
//		Simple embedded webserver content, see html.go
//	POST /
//		Submit a new query with given stenographer connections and source override
//	GET /status
//		Return a JSON object of current job status across all stenographer connections
//	GET /conns
//		Return a JSON object of all stenographer connections
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// get for GET /status or /
	if r.Method == "GET" {
		switch r.URL.Path {
		case "/status":
			s := status()
			w.Write([]byte(s))
			return
		case "/":
			// root
			w.Write([]byte(index))
			return
		case "/conns":
			c := conns()
			w.Write([]byte(c))
			return
		}
	}

	if r.Method != "POST" {
		return
	}
	if r.Close {
		defer r.Body.Close()
	}

	var b bytes.Buffer
	io.Copy(&b, r.Body)
	var p poster
	json.Unmarshal(b.Bytes(), &p)

	debugout("query received: %v\n", p.Q)

	ss, _ := strconv.Atoi(p.S)

	var wg sync.WaitGroup

	// create a new job
	jobLock.Lock()
	j := &job{
		ID:     jcount,
		Query:  p.Q,
		Source: uint32(ss),
		Conns:  p.C,
	}
	jcount++
	jobs = append(jobs, j)
	jobLock.Unlock()

	for _, v := range p.C {
		if h, ok := stenos[v]; ok {
			url := h.url + "/query"
			qq := bytes.NewBufferString(p.Q)
			resp, err := h.client.Post(url, "text/plain", qq)
			if err != nil {
				lg.Error("%v", err)
				continue
			}
			wg.Add(1)
			go h.processPcap(resp.Body, j, &wg)
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("%v", j.ID)))

	go func() {
		wg.Wait()
		removeJob(j.ID)
	}()
}

func removeJob(j uint) {
	jobLock.Lock()
	defer jobLock.Unlock()

	for i, v := range jobs {
		if v.ID == j {
			jobs = append(jobs[:i], jobs[i+1:]...)
		}
	}
}

func (h *handlerConfig) processPcap(in io.ReadCloser, j *job, wg *sync.WaitGroup) {
	// stream in the pcap, batch processing packets to the ingester
	defer in.Close()
	defer wg.Done()

	p, err := pcap.NewReader(in)
	if err != nil {
		lg.Error("%v", err)
		return
	}

	for {
		data, ci, err := p.ReadPacketData()
		if err != nil {
			if err != io.EOF {
				lg.Error("%v", err)
			}
			if len(data) == 0 {
				break
			}
		}

		var ts entry.Timestamp
		if !h.ignoreTimestamps {
			if ci.Timestamp.IsZero() {
				ts = entry.Now()
			} else {
				ts = entry.FromStandard(ci.Timestamp)
			}
		} else {
			ts = entry.Now()
		}

		ent := &entry.Entry{
			SRC:  h.src,
			TS:   ts,
			Tag:  h.tag,
			Data: data,
		}
		if j.Source != 0 {
			b, err := config.ParseSource(fmt.Sprintf("%v", j.Source))
			if err == nil {
				ent.SRC = b
			}
		}

		j.lock.Lock()
		j.Bytes += uint(len(data))
		j.lock.Unlock()

		if err = h.proc.Process(ent); err != nil {
			debugout("%v", err)
			lg.Error("Sending message: %v", err)
			return
		}
	}
}

func status() []byte {
	jobLock.Lock()
	defer jobLock.Unlock()

	ret, err := json.Marshal(jobs)
	if err != nil {
		lg.Error("%v", err)
		return nil
	}
	return ret
}

func conns() []byte {
	var sc []string
	for k, _ := range stenos {
		sc = append(sc, k)
	}
	ret, err := json.Marshal(sc)
	if err != nil {
		lg.Error("%v", err)
		return nil
	}
	return ret
}
