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
	"context"
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
	"runtime/debug"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"

	pcap "github.com/google/gopacket/pcapgo"
)

const (
	defaultConfigLoc      = `/opt/gravwell/etc/packet_fleet.conf`
	defaultConfigDLoc     = `/opt/gravwell/etc/packet_fleet.conf.d`
	ingesterName          = `PacketFleet`
	appName               = `packetfleet`
	batchSize             = 512
	maxDataSize       int = 8 * 1024 * 1024
	initDataSize      int = 512 * 1024
)

var (
	cpuprofile     = flag.String("cpuprofile", "", "write cpu profile to file")
	confLoc        = flag.String("config-file", defaultConfigLoc, "Location for configuration file")
	confdLoc       = flag.String("config-overlays", defaultConfigDLoc, "Location for configuration overlay files")
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
	ctx              context.Context

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
	validate.ValidateConfig(GetConfig, *confLoc, *confdLoc)
	var fp string
	var err error
	if *stderrOverride != `` {
		fp = filepath.Join(`/dev/shm/`, *stderrOverride)
	}
	cb := func(w io.Writer) {
		version.PrintVersion(w)
		ingest.PrintVersion(w)
		log.PrintOSInfo(w)
	}
	if lg, err = log.NewStderrLoggerEx(fp, cb); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get stderr logger: %v\n", err)
		os.Exit(-1)
	}
	lg.SetAppname(appName)

	v = *verbose
}

func main() {
	debug.SetTraceback("all")
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			lg.FatalCode(0, "failed to open profile file", log.KV("path", *cpuprofile), log.KVErr(err))
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	cfg, err := GetConfig(*confLoc, *confdLoc)
	if err != nil {
		lg.FatalCode(0, "failed to get configuration", log.KVErr(err))
		return
	}

	if len(cfg.Global.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Global.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "failed to open log file", log.KV("path", cfg.Global.Log_File), log.KVErr(err))
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("failed to add a writer", log.KVErr(err))
		}
		if len(cfg.Global.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Global.Log_Level); err != nil {
				lg.FatalCode(0, "invalid Log Level", log.KV("loglevel", cfg.Global.Log_Level), log.KVErr(err))
			}
		}
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.FatalCode(0, "failed to get tags from configuration", log.KVErr(err))
		return
	}
	conns, err := cfg.Global.Targets()
	if err != nil {
		lg.FatalCode(0, "failed to get backend targets from configuration", log.KVErr(err))
		return
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	lmt, err := cfg.Global.RateLimit()
	if err != nil {
		lg.FatalCode(0, "failed to get rate limit from configuration", log.KVErr(err))
		return
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	//fire up the ingesters
	debugout("INSECURE skip TLS certificate verification: %v\n", cfg.Global.InsecureSkipTLSVerification())
	id, ok := cfg.Global.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID")
	}
	igCfg := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.Global.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Global.Secret(),
		VerifyCert:         !cfg.Global.InsecureSkipTLSVerification(),
		IngesterName:       ingesterName,
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       id.String(),
		IngesterLabel:      cfg.Global.Label,
		RateLimitBps:       lmt,
		Logger:             lg,
		CacheDepth:         cfg.Global.Cache_Depth,
		CachePath:          cfg.Global.Ingest_Cache_Path,
		CacheSize:          cfg.Global.Max_Ingest_Cache,
		CacheMode:          cfg.Global.Cache_Mode,
		LogSourceOverride:  net.ParseIP(cfg.Global.Log_Source_Override),
	}
	igst, err = ingest.NewUniformMuxer(igCfg)
	if err != nil {
		lg.Fatal("failed build our ingest system", log.KVErr(err))
		return
	}

	defer igst.Close()
	debugout("Started ingester muxer\n")
	if cfg.Global.SelfIngest() {
		lg.AddRelay(igst)
	}
	if err := igst.Start(); err != nil {
		lg.Fatal("failed start our ingest system", log.KVErr(err))
		return
	}
	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Global.Timeout()); err != nil {
		lg.FatalCode(0, "timeout waiting for backend connections", log.KV("timeout", cfg.Global.Timeout()), log.KVErr(err))
		return
	}
	debugout("Successfully connected to ingesters\n")

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		lg.FatalCode(0, "failed to set configuration for ingester state messages", log.KVErr(err))
	}

	var wg sync.WaitGroup

	// setup stenographer connections
	stenos = make(map[string]*handlerConfig)

	ctx, cancel := context.WithCancel(context.Background())

	for k, v := range cfg.Stenographer {
		var src net.IP

		if v.Source_Override != `` {
			src = net.ParseIP(v.Source_Override)
			if src == nil {
				lg.Fatal("invalid Source-Override", log.KV("sourceoverride", v.Source_Override))
			}
		} else if cfg.Global.Source_Override != `` {
			// global override
			src = net.ParseIP(cfg.Global.Source_Override)
			if src == nil {
				lg.Fatal("invalid Global Source-Override", log.KV("sourceoverride", cfg.Global.Source_Override))
			}
		}

		//get the tag for this listener
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.Fatal("failed to resolve tag", log.KV("handler", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
		}

		hcfg := &handlerConfig{
			url:              strings.TrimRight(v.URL, "/"),
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
			ctx:              ctx,
		}

		if hcfg.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Fatal("preprocessor error", log.KV("handler", k), log.KV("preprocessor", v.Preprocessor), log.KVErr(err))
		}

		// Load client cert
		cert, err := tls.LoadX509KeyPair(hcfg.clientCert, hcfg.clientKey)
		if err != nil {
			lg.FatalCode(0, "failed to load certificate", log.KV("certfile", hcfg.clientCert), log.KV("keyfile", hcfg.clientKey), log.KVErr(err))
			return
		}

		// Load CA cert
		caCert, err := ioutil.ReadFile(hcfg.caCert)
		if err != nil {
			lg.FatalCode(0, "failed to load CA certificate", log.KV("cacert", hcfg.caCert), log.KVErr(err))
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
	go s.listener(cfg.Global.Listen_Address, cfg.Global.Use_TLS, cfg.Global.Server_Cert, cfg.Global.Server_Key)

	debugout("Running\n")

	//listen for signals so we can close gracefully
	utils.WaitForQuit()

	cancel()

	if err := igst.Sync(time.Second); err != nil {
		lg.Error("failed to sync", log.KVErr(err))
	}
	if err := igst.Close(); err != nil {
		lg.Error("failed to close", log.KVErr(err))
	}
}

func debugout(format string, args ...interface{}) {
	if !v {
		return
	}
	fmt.Printf(format, args...)
}

func (s *server) listener(laddr string, usetls bool, c, k string) {
	// start our listener
	srv := &http.Server{
		Addr:    laddr,
		Handler: s,
	}
	if usetls {
		if err := srv.ListenAndServeTLS(c, k); err != nil {
			lg.Error("failed to listenAndServeTLS", log.KVErr(err))
		}
	} else {
		if err := srv.ListenAndServe(); err != nil {
			lg.Error("failed to listenAndServe", log.KVErr(err))
		}
	}
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
		case "/conns":
			c := conns()
			w.Write([]byte(c))
			return
		default:
			// root
			w.Write([]byte(index))
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
				lg.Error("client post error", log.KV("url", url), log.KVErr(err))
				continue
			}
			if resp.StatusCode != 200 {
				resp.Body.Close()
				lg.Error("invalid query", log.KV("status", resp.StatusCode))
				w.WriteHeader(http.StatusBadRequest)
				removeJob(j.ID)
				return
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
		lg.Error("failed to get new PCAP reader", log.KVErr(err))
		return
	}

	for {
		data, ci, err := p.ReadPacketData()
		if err != nil {
			if err != io.EOF {
				lg.Error("failed to read packet data", log.KVErr(err))
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

		if err = h.proc.ProcessContext(ent, h.ctx); err != nil {
			debugout("%v", err)
			lg.Error("failed to send message", log.KVErr(err))
			return
		}
	}
}

func status() []byte {
	jobLock.Lock()
	defer jobLock.Unlock()

	ret, err := json.Marshal(jobs)
	if err != nil {
		lg.Error("failed to marshal JSON", log.KVErr(err))
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
		lg.Error("failed to marshal JSON", log.KVErr(err))
		return nil
	}
	return ret
}
