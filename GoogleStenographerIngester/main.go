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
	defaultConfigLoc     = `/opt/gravwell/etc/stenographer_ingester.conf`
	ingesterName         = `stenographerIngester`
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
	laddr            string
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
	done             chan bool
	proc             *processors.ProcessorSet

	client  *http.Client
	jobs    []*job
	jcount  uint
	jobLock sync.Mutex
}

type poster struct {
	Q string
	S string
}

type job struct {
	ID     uint
	Bytes  uint
	Query  string
	Source uint32
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

	lmt, err := cfg.RateLimit()
	if err != nil {
		lg.FatalCode(0, "Failed to get rate limit from configuration: %v\n", err)
		return
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	//fire up the ingesters
	debugout("INSECURE skip TLS certificate verification: %v\n", cfg.InsecureSkipTLSVerification())
	id, ok := cfg.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID\n")
	}
	igCfg := ingest.UniformMuxerConfig{
		Destinations:    conns,
		Tags:            tags,
		Auth:            cfg.Secret(),
		LogLevel:        cfg.LogLevel(),
		VerifyCert:      !cfg.InsecureSkipTLSVerification(),
		IngesterName:    ingesterName,
		IngesterVersion: version.GetVersion(),
		IngesterUUID:    id.String(),
		RateLimitBps:    lmt,
		Logger:          lg,
	}
	if cfg.EnableCache() {
		igCfg.EnableCache = true
		igCfg.CacheConfig.FileBackingLocation = cfg.LocalFileCachePath()
		igCfg.CacheConfig.MaxCacheSize = cfg.MaxCachedData()
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
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "Timedout waiting for backend connections: %v\n", err)
		return
	}
	debugout("Successfully connected to ingesters\n")
	var wg sync.WaitGroup
	done := make(chan bool)

	// make sqs connections
	for k, v := range cfg.Stenographer {
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

		hcfg := &handlerConfig{
			laddr:            v.Listen_Address,
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
			done:             done,
		}

		if hcfg.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Fatal("Preprocessor failure: %v", err)
		}

		wg.Add(1)
		go stenoRunner(hcfg)
	}

	debugout("Running\n")

	//listen for signals so we can close gracefully
	utils.WaitForQuit()

	// wait for graceful shutdown
	close(done)
	wg.Wait()

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

func stenoRunner(hcfg *handlerConfig) {
	defer hcfg.wg.Done()

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

	// start our listener
	s := &http.Server{
		Addr:    hcfg.laddr,
		Handler: hcfg,
	}
	go func() {
		lg.Error("%v", s.ListenAndServe())
	}()

	<-hcfg.done
}

func (h *handlerConfig) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// get for GET /status or /
	if r.Method == "GET" {
		switch r.URL.Path {
		case "/status":
			s := h.Status()
			w.Write([]byte(s))
			return
		case "/":
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
	qq := bytes.NewBufferString(p.Q)

	debugout("query received: %v\n", p.Q)

	// literally just POST the request's body onto stenographer...
	url := h.url + "/query"
	resp, err := h.client.Post(url, "text/plain", qq)
	if err != nil {
		lg.Error("%v", err)
		return
	}

	ss, _ := strconv.Atoi(p.S)

	// create a new job
	h.jobLock.Lock()
	j := &job{
		ID:     h.jcount,
		Query:  p.Q,
		Source: uint32(ss),
	}
	h.jcount++
	h.jobs = append(h.jobs, j)
	h.jobLock.Unlock()

	// we have a body response, 200 OK the client and fork off a goroutine
	// to process the body
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("%v", j.ID)))

	go h.processPcap(resp.Body, j)
}

func (h *handlerConfig) removeJob(j uint) {
	h.jobLock.Lock()
	defer h.jobLock.Unlock()

	for i, v := range h.jobs {
		if v.ID == j {
			h.jobs = append(h.jobs[:i], h.jobs[i+1:]...)
		}
	}
}

func (h *handlerConfig) processPcap(in io.ReadCloser, j *job) {
	// stream in the pcap, batch processing packets to the ingester
	defer in.Close()
	defer h.removeJob(j.ID)

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

		j.Bytes += uint(len(data))

		if err = h.proc.Process(ent); err != nil {
			debugout("%v", err)
			lg.Error("Sending message: %v", err)
			return
		}
	}
	time.Sleep(15 * time.Second)
}

func (h *handlerConfig) Status() []byte {
	h.jobLock.Lock()
	defer h.jobLock.Unlock()

	ret, err := json.Marshal(h.jobs)
	if err != nil {
		lg.Error("%v", err)
		return nil
	}
	return ret
}
