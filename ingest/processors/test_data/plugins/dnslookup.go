package main

import (
	"context"
	"errors"
	"fmt"
	"gravwell" //package expose the builtin plugin funcs
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/miekg/dns"
)

const (
	PluginName                         = "dnslookup"
	defaultAppendFormat                = ` resolved: %v`
	defaultTimeout                     = 500 * time.Millisecond
	defaultWorkerCount                 = 8
	defaultMaxCacheCount               = 64 * 1024 // 64k isn't crazy
	maxRecursion         int           = 32        //this is crazy
	workerBuffer         int           = 16        //buffer size of requests PER WORKER
	cacheScanDur         time.Duration = 30 * time.Second
)

const ( //config names (remember that a '-' in the config file becomes a '_' in the name
	regexConfigName            = `Regex`
	regexExtractHostName       = `Regex_Extraction_Host`
	regexExtractRecordTypeName = `Regex_Extraction_Record_Type`
	dnsServerConfigName        = `DNS_Server`
	appendFormatConfigName     = `Append_Format`
	timeoutConfigName          = `Timeout`
	workerCountName            = `Worker_Count`
	maxCacheCountName          = `Max_Cache_Count`
	synchronousName            = `Synchronous`
	debugModeName              = `Debug`
)

var (
	mtx         *sync.Mutex
	ctx, cancel = context.WithCancel(context.Background())
	cfg         LookupConfig
	tg          gravwell.Tagger
	ready       bool
	running     bool

	ErrNotReady       = errors.New("not ready")
	ErrAlreadyStarted = errors.New("already started")

	workers *workerGroup
)

func main() {
	mtx = &sync.Mutex{}
	//start the plugin by providing the name, config, process, and flush functions
	if err := gravwell.Execute(PluginName, Config, Start, Close, Process, Flush); err != nil {
		panic(fmt.Sprintf("Failed to execute dynamic plugin %s - %v\n", PluginName, err))
	}
}

type LookupConfig struct {
	Regex                     string
	RegexExtractionHost       string
	RegexExtractionRecordType string
	DNSServer                 []string
	AppendFormat              string
	Timeout                   time.Duration
	Debug                     bool
	WorkerCount               int64
	MaxCacheCount             int64
	Synchronous               bool
	re                        *regexp.Regexp
	hostIdx                   int
	recordIdx                 int
}

// The Config function
func Config(cm gravwell.ConfigMap, tgr gravwell.Tagger) (err error) {
	var temp string
	if cm == nil || tgr == nil {
		err = errors.New("bad parameters")
	}
	cfg.Debug, _ = cm.GetBool(debugModeName)
	cfg.Synchronous, _ = cm.GetBool(synchronousName)

	if cfg.Regex, err = cm.GetString(regexConfigName); err != nil {
		return fmt.Errorf("Failed to get regex config variable: %w", err)
	} else if cfg.RegexExtractionHost, err = cm.GetString(regexExtractHostName); err != nil {
		return fmt.Errorf("Failed to get regex field extraction name variable: %w", err)
	} else if cfg.RegexExtractionRecordType, err = cm.GetString(regexExtractRecordTypeName); err != nil {
		return fmt.Errorf("Failed to get regex field extraction record type variable: %w", err)
	} else if cfg.WorkerCount, err = cm.GetInt(workerCountName); err != nil {
		return fmt.Errorf("Failed to get worker count: %w", err)
	} else if cfg.DNSServer, err = cm.GetStringSlice(dnsServerConfigName); err != nil {
		return fmt.Errorf("Failed to get DNS_Server: %w", err)
	} else if cfg.Regex == `` || cfg.RegexExtractionHost == `` || cfg.RegexExtractionRecordType == `` {
		return fmt.Errorf("Regex and Regex-Extraction-Name are required")
	} else if cfg.MaxCacheCount, err = cm.GetInt(maxCacheCountName); err != nil {
		return fmt.Errorf("invalid Max-Cache-Count, must be > 0: %w", err)
	} else if cfg.WorkerCount < 0 {
		return fmt.Errorf("invalid Worker-Count %d", cfg.WorkerCount)
	} else if len(cfg.DNSServer) == 0 {
		return fmt.Errorf("missing DNS-Server configuration, please specify at least one DNS-Server")
	} else if cfg.MaxCacheCount < 0 {
		return fmt.Errorf("invalid Max-Cache-Count %d, must be >= 0", cfg.MaxCacheCount)
	} else {
		debug("Regex %q with lookup on %s\n", cfg.Regex, cfg.RegexExtractionHost)
	}
	if cfg.WorkerCount == 0 {
		cfg.WorkerCount = defaultWorkerCount
	}
	if cfg.MaxCacheCount == 0 {
		cfg.MaxCacheCount = defaultMaxCacheCount
	}
	debug("using %d workers\n", cfg.WorkerCount)
	debug("using %d DNS servers\n", len(cfg.DNSServer))

	if temp, _ = cm.GetString(timeoutConfigName); temp != `` {
		if cfg.Timeout, err = time.ParseDuration(temp); err != nil {
			return fmt.Errorf("Invalid Timeout config (%s): %w", temp, err)
		}
	} else {
		cfg.Timeout = defaultTimeout
	}
	debug("timeout set to %v\n", cfg.Timeout)

	if cfg.AppendFormat, _ = cm.GetString(appendFormatConfigName); cfg.AppendFormat == `` {
		cfg.AppendFormat = defaultAppendFormat
	}
	debug("Append Format: %v\n", cfg.AppendFormat)

	//parse the regex and make sure it has the extraction name
	if cfg.re, err = regexp.Compile(cfg.Regex); err != nil {
		return fmt.Errorf("Failed to compile regular expression (%s): %w", cfg.Regex, err)
	} else if cfg.hostIdx = cfg.re.SubexpIndex(cfg.RegexExtractionHost); cfg.hostIdx == -1 {
		return fmt.Errorf("Regex-Extraction-Host %q is not present in Regex %s",
			cfg.RegexExtractionHost, cfg.Regex)
	} else if cfg.recordIdx = cfg.re.SubexpIndex(cfg.RegexExtractionRecordType); cfg.recordIdx == -1 {
		return fmt.Errorf("Regex-Extraction-Record-Type %q is not present in Regex %s",
			cfg.RegexExtractionRecordType, cfg.Regex)
	}

	//ok we have our regex, our append format, our index of the extracted name
	//maybe even a specific DNS server to query
	ready = true
	debug("ready\n")
	return
}

func Start() (err error) {
	mtx.Lock()
	if workers != nil {
		err = ErrAlreadyStarted
	} else if workers, err = newWorkerGroup(cfg.DNSServer, int(cfg.WorkerCount), workerBuffer, cfg.Timeout, ctx); err == nil {
		err = startWorkerGroup(workers)
	}
	mtx.Unlock()
	return
}

func Close() (err error) {
	mtx.Lock()
	if workers != nil {
		err = stopWorkerGroup(workers)
	}
	mtx.Unlock()
	return
}

func Flush() []*entry.Entry {
	var set []*entry.Entry
	mtx.Lock()
	if workers != nil {
		workers.jobCounter.Wait()
		set = workerGroupConsumeReadySet(workers)
	}
	mtx.Unlock()
	return set //we don't hold on to anything
}

func Process(ents []*entry.Entry) (ret []*entry.Entry, err error) {
	mtx.Lock()
	if !ready {
		err = ErrNotReady
	} else {
		for i := range ents {
			addWorkerGroupJob(workers, ents[i])
		}
		ret = workerGroupConsumeReadySet(workers)
	}
	mtx.Unlock()
	return
}

func makeIpString(ips []net.IP) string {
	if len(ips) == 0 {
		return ``
	} else if len(ips) == 1 {
		return ips[0].String()
	}

	//espensive join
	var sb strings.Builder
	for i := range ips {
		if i > 0 {
			sb.WriteRune(' ')
		}
		sb.WriteString(ips[i].String())
	}
	return sb.String()
}

func debug(format string, vals ...interface{}) {
	if cfg.Debug {
		fmt.Printf(format, vals...)
	}
}

type request struct {
	ent  *entry.Entry
	host string
	aaaa bool
}

func newWorkerGroup(nsaddr []string, cnt, wb int, to time.Duration, ctx context.Context) (wg *workerGroup, err error) {
	if len(nsaddr) <= 0 {
		err = errors.New("invalid nameservers")
		return
	}
	if cnt <= 0 || wb <= 0 {
		err = errors.New("invalid count or worker buffer")
		return
	}
	workers := make([]*worker, cnt)
	for i := range workers {
		if workers[i], err = newWorker(nsaddr[i%len(nsaddr)], to, ctx); err != nil {
			return
		}
	}
	wg = &workerGroup{
		inputMtx:   &sync.Mutex{},
		outputMtx:  &sync.Mutex{},
		wg:         &sync.WaitGroup{},
		jobCounter: &sync.WaitGroup{},
		cache:      newDnsCache(int(cfg.MaxCacheCount)),
		workers:    workers,
		input:      make(chan request, cnt*wb),
		retry:      make(chan request, cnt),
		output:     make(chan *entry.Entry, cnt*wb),
	}
	return
}

func startWorkerGroup(wg *workerGroup) (err error) {
	wg.inputMtx.Lock()
	if wg.started {
		wg.inputMtx.Unlock()
		return errors.New("already started")
	} else {
		wg.started = true
		for i := range wg.workers {
			wg.workers[i].wg.Add(1)
			go workerRoutine(wg.workers[i], wg.cache, wg.retry, wg.input, wg.output)
		}
		wg.wg.Add(1)
		go workerGroupResponseHandler(wg, wg.output)
		wg.running = true
	}
	wg.inputMtx.Unlock()
	return nil
}

func stopWorkerGroup(wg *workerGroup) error {
	wg.inputMtx.Lock()
	if !wg.started {
		wg.inputMtx.Unlock()
		return errors.New("not started")
	}
	wg.running = false
	close(wg.input)
	wg.inputMtx.Unlock() //unlock so that workers and consumer can unlock
	for _, w := range wg.workers {
		w.wg.Wait()
	}
	//take the first worker and make him finish out all the retries
	w := wg.workers[0]
	for len(wg.retry) > 0 {
		r := <-wg.retry
		ips, _, _ := resolve(w, r.host, r.aaaa)
		enrichEntry(r.ent, ips)
		wg.output <- r.ent
	}
	wg.inputMtx.Lock() //relock to close out workers

	//close the workers
	for _, w := range wg.workers {
		closeWorker(w)
	}
	//close output
	close(wg.output)
	wg.inputMtx.Unlock()
	wg.wg.Wait()
	return nil
}

func extract(v []byte) (host string, aaaa, ok bool) {
	if subs := cfg.re.FindSubmatch(v); subs == nil || len(subs) <= cfg.hostIdx || len(subs) <= cfg.recordIdx {
		debug("Regex failed on: %q\n", string(v))
		return
	} else {
		host = string(subs[cfg.hostIdx])
		if rt := string(subs[cfg.recordIdx]); rt == `AAAA` {
			aaaa = true
		}
		ok = true
	}
	return
}

func enrichEntry(ent *entry.Entry, ips []net.IP) {
	if ent != nil && len(ips) > 0 {
		ips := makeIpString(ips)
		ent.Data = append(ent.Data, []byte(fmt.Sprintf(cfg.AppendFormat, ips))...)
	}
}

func addWorkerGroupJob(wg *workerGroup, ent *entry.Entry) (ok bool) {
	if ent == nil {
		return
	}
	var r request
	r.host, r.aaaa, ok = extract(ent.Data)
	wg.inputMtx.Lock()
	if wg.started && wg.running {
		r.ent = ent
		workers.jobCounter.Add(1)
		wg.input <- r
		ok = true
	}
	wg.inputMtx.Unlock()
	return
}

func workerGroupAppendReadySet(wg *workerGroup, set []*entry.Entry) {
	if len(set) == 0 {
		return
	}
	wg.outputMtx.Lock()
	wg.ready = append(wg.ready, set...)
	workers.jobCounter.Add(-1 * len(set))
	wg.outputMtx.Unlock()
}

func workerGroupConsumeReadySet(wg *workerGroup) (set []*entry.Entry) {
	wg.outputMtx.Lock()
	if len(wg.ready) > 0 {
		set = append(set, wg.ready...)
		wg.ready = wg.ready[0:0]
	}
	wg.outputMtx.Unlock()
	return
}

func workerGroupResponseHandler(wg *workerGroup, in chan *entry.Entry) {
	for {
		var set []*entry.Entry
		ent, ok := <-in
		if !ok {
			break
		} else if ent != nil {
			set = []*entry.Entry{ent}
		}
		for len(in) > 0 {
			if r, ok := <-in; !ok {
				break
			} else if r != nil {
				set = append(set, r)
			}
		}
		workerGroupAppendReadySet(wg, set)
	}
	wg.wg.Done()
}

type workerGroup struct {
	inputMtx   *sync.Mutex
	outputMtx  *sync.Mutex
	wg         *sync.WaitGroup
	jobCounter *sync.WaitGroup
	cache      *dnsCache
	started    bool
	running    bool
	workers    []*worker
	input      chan request
	retry      chan request
	output     chan *entry.Entry

	ready []*entry.Entry
}

type worker struct {
	ns  string
	mtx *sync.Mutex
	wg  *sync.WaitGroup
	to  time.Duration
	ctx context.Context
	c   *dns.Client
	co  *dns.Conn
	m   *dns.Msg
}

func newWorker(ns string, to time.Duration, ctx context.Context) (w *worker, err error) {
	w = &worker{
		ns:  ns,
		ctx: ctx,
		mtx: &sync.Mutex{},
		wg:  &sync.WaitGroup{},
		to:  to,
		c: &dns.Client{
			Net:          `udp`,
			DialTimeout:  to,
			ReadTimeout:  to,
			WriteTimeout: to,
		},
		m: &dns.Msg{
			MsgHdr: dns.MsgHdr{
				Opcode:             dns.OpcodeQuery,
				RecursionDesired:   true,
				RecursionAvailable: true,
			},
			Compress: true,
			Question: make([]dns.Question, 1),
		},
		co: new(dns.Conn),
	}
	if w.co.Conn, err = net.DialTimeout(w.c.Net, ns, to); err != nil {
		w = nil
	}
	return
}

func workerRoutine(w *worker, cache *dnsCache, retry, in chan request, out chan *entry.Entry) {
	defer w.wg.Done()
	var processed int
	for {
		var r request
		var ok bool
		select {
		case r, ok = <-in:
		case r, ok = <-retry:
		}
		if !ok {
			break
		}
		if ips, ok := cacheGet(cache, r.host, r.aaaa); ok {
			enrichEntry(r.ent, ips)
			out <- r.ent
			processed++
		} else if ips, ttl, err := resolve(w, r.host, r.aaaa); err != nil {
			//failed to resolve, put it back on the reque channel, wait 1 second and try again
			retry <- r
			time.Sleep(time.Second)
		} else {
			enrichEntry(r.ent, ips)
			out <- r.ent
			processed++
			cacheSet(cache, r.host, r.aaaa, ips, ttl)
		}
	}
	//try to handle the retries
	for {
		select {
		case r, ok := <-retry:
			if !ok {
				return
			} else if ips, _, err := resolve(w, r.host, r.aaaa); err != nil {
				retry <- r //kick it back into the retry loop and bail
				debug("Resolution error on %s %v\n", r.host, err)
				return
			} else {
				enrichEntry(r.ent, ips)
				out <- r.ent
				processed++
			}
		default:
			return //retry is empty, leave
		}
	}
}

func closeWorker(w *worker) (err error) {
	w.mtx.Lock()
	if w != nil && w.co != nil && w.co.Conn != nil {
		err = w.co.Conn.Close()
	}
	w.mtx.Unlock()
	w.wg.Wait()
	return
}

func reinitConn(w *worker) (err error) {
	if w != nil && w.co != nil {
		if w.co.Conn != nil {
			w.co.Conn.Close()
		}
		switch w.c.Net {
		case `udp`:
			w.c.Net = `tcp`
		case `tcp`:
			fallthrough
		default:
			w.c.Net = `udp`
		}
		w.co.Conn, err = net.DialTimeout(w.c.Net, w.ns, w.to)
	}
	return
}

// exchange does a full synchronous request and response on the current connection
// caller must hold the lock
func exchange(w *worker, host string, aaaa, canRetry bool) (r *dns.Msg, err error) {
	tp := dns.TypeA
	if aaaa {
		tp = dns.TypeAAAA
	}
	w.m.Question[0] = dns.Question{Name: dns.Fqdn(host), Qtype: tp, Qclass: dns.ClassINET}
	w.m.Id = dns.Id()
	if w.co.Conn == nil {
		if err = reinitConn(w); err != nil {
			return
		}
	}
	if err = w.co.SetWriteDeadline(time.Now().Add(w.to)); err != nil {
		return
	} else if err = w.co.WriteMsg(w.m); err != nil {
		if canRetry {
			if lerr := reinitConn(w); lerr == nil {
				r, err = exchange(w, host, aaaa, false)
			}
		}
	} else if err = w.co.SetReadDeadline(time.Now().Add(w.to)); err != nil {
		return
	} else if r, err = w.co.ReadMsg(); err != nil {
		if canRetry {
			if lerr := reinitConn(w); lerr == nil {
				r, err = exchange(w, host, aaaa, false)
			}
		}
	}
	return
}

func resolve(w *worker, host string, aaaa bool) (ips []net.IP, ttl time.Duration, err error) {
	var r *dns.Msg
	w.mtx.Lock()
	defer w.mtx.Unlock()
	if w.co == nil {
		if err = reinitConn(w); err != nil {
			return
		}
	}
	if r, err = exchange(w, host, aaaa, true); err != nil {
		return
	}
	//check if the response is Truncated, if so, reinit as a TCP connection and try again
	if r.Truncated && w.c.Net == `udp` {
		if err = reinitConn(w); err != nil {
			return
		} else if r, err = exchange(w, host, aaaa, false); err != nil {
			return
		}
		//reinit worked, and so did exchange, do not return
	}
	ips, ttl, err = handleResponse(w, w.m, r)
	return
}

// handleResponse will walk all the rsponses and respond appropriately, we currently only handle A, AAAA, and CNAME
// resposnes, everything else is ignored, this means if we get a refuse or
func handleResponse(w *worker, m, r *dns.Msg) (ips []net.IP, ttl time.Duration, err error) {
	for _, v := range r.Answer {
		if a, ok := v.(*dns.A); ok {
			if ip := a.A; ip != nil {
				ips = dedupAppend(ips, ip)
				if sec := a.Hdr.Ttl; sec > 0 {
					lttl := time.Duration(sec) * time.Second
					if ttl == 0 || lttl < ttl {
						ttl = lttl
					}
				}
			}
		} else if a, ok := v.(*dns.AAAA); ok {
			if ip := a.AAAA; ip != nil {
				ips = dedupAppend(ips, ip)
				if sec := a.Hdr.Ttl; sec > 0 {
					lttl := time.Duration(sec) * time.Second
					if ttl == 0 || lttl < ttl {
						ttl = lttl
					}
				}
			}
		} else if cn, ok := v.(*dns.CNAME); ok {
			if cn.Target != `` {
				if lips, ttlSec := recurseResolveRunner(cn.Hdr.Name, w.co, w.m, 0, nil, 0); len(lips) > 0 {
					ips = dedupAppendSet(ips, lips)
					if ttlSec > 0 {
						lttl := time.Duration(ttlSec) * time.Second
						if ttl == 0 || lttl < ttl {
							ttl = lttl
						}
					}
				}
			}
		}
	}
	return
}

func dedupAppend(ips []net.IP, ip net.IP) []net.IP {
	for _, v := range ips {
		if v.Equal(ip) {
			return ips
		}
	}
	return append(ips, ip)
}

func dedupAppendSet(ips []net.IP, set []net.IP) []net.IP {
	for _, ip := range set {
		ips = dedupAppend(ips, ip)
	}
	return ips
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if ne, ok := err.(net.Error); ok {
		return ne.Timeout()
	}
	return false
}

func recurseResolve(nm string, co *dns.Conn, m *dns.Msg) ([]net.IP, uint32) {
	return recurseResolveRunner(nm, co, m, 0, nil, 0)
}

func recurseResolveRunner(nm string, co *dns.Conn, m *dns.Msg, depth int, inIPs []net.IP, intTTL uint32) (out []net.IP, ttl uint32) {
	var r *dns.Msg
	var err error
	out = inIPs
	ttl = intTTL
	if depth > maxRecursion {
		return
	}
	//do linear query
	m.Question[0] = dns.Question{Name: dns.Fqdn(nm), Qtype: dns.TypeA, Qclass: dns.ClassINET}
	m.Id = dns.Id()
	co.SetWriteDeadline(time.Now().Add(cfg.Timeout))
	if err = co.WriteMsg(m); err != nil {
		return
	}
	co.SetReadDeadline(time.Now().Add(cfg.Timeout))
	if r, err = co.ReadMsg(); err != nil {
		return
	}
	//iterate over responses
	for _, v := range r.Answer {
		switch a := v.(type) {
		case *dns.A:
			if a.A != nil {
				out = dedupAppend(out, a.A)
				if sec := a.Hdr.Ttl; sec > 0 && sec < ttl {
					ttl = sec
				}
			}
		case *dns.AAAA:
			if a.AAAA != nil {
				out = dedupAppend(out, a.AAAA)
				if sec := a.Hdr.Ttl; sec > 0 && sec < ttl {
					ttl = sec
				}
			}
		case *dns.CNAME:
			if a.Target != `` {
				out, ttl = recurseResolveRunner(a.Target, co, m, depth+1, out, ttl)
			}
		}
	}
	return
}

type cacheval struct {
	expire time.Time
	ips    []net.IP
}

type dnsCache struct {
	mtx      *sync.Mutex
	max      int
	mp       map[string]cacheval
	lastScan time.Time
}

func cachekey(host string, aaaa bool) string {
	if aaaa {
		return host + `AAAA`
	}
	return host + `A`
}

func newDnsCache(max int) *dnsCache {
	if max <= 0 {
		max = defaultMaxCacheCount
	}
	return &dnsCache{
		mtx: &sync.Mutex{},
		max: max,
		//mp:  map[cachekey]cacheval{},
		mp: map[string]cacheval{},
	}
}

func cacheGet(dc *dnsCache, name string, aaaa bool) (ips []net.IP, ok bool) {
	key := cachekey(name, aaaa)
	var val cacheval
	dc.mtx.Lock()
	if dur := time.Since(dc.lastScan); dur > cacheScanDur || len(dc.mp) > dc.max {
		cacheScan(dc)
	}
	if val, ok = dc.mp[key]; ok {
		ips = val.ips
	}
	dc.mtx.Unlock()
	return
}

func cacheSet(dc *dnsCache, name string, aaaa bool, ips []net.IP, ttl time.Duration) {
	key := cachekey(name, aaaa)
	if ttl <= 0 {
		return
	}
	val := cacheval{expire: time.Now().Add(ttl), ips: ips}
	dc.mtx.Lock()
	dc.mp[key] = val
	dc.mtx.Unlock()
}

func cacheScan(dc *dnsCache) {
	now := time.Now()
	dc.lastScan = now
	//return
	for k, v := range dc.mp {
		if v.expire.Before(now) {
			delete(dc.mp, k)
		}
	}
	if len(dc.mp) > dc.max {
		//nuke 10% of the entries
		toKill := len(dc.mp) / 10
		for k := range dc.mp {
			delete(dc.mp, k)
			toKill--
			if toKill <= 0 {
				break
			}
		}
	}
}
