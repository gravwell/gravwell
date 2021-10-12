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
	PluginName              = "dnslookup"
	defaultAppendFormat     = ` resolved: %v`
	defaultTimeout          = 500 * time.Millisecond
	defaultWorkerCount      = 8
	maxRecursion        int = 32 //this is crazy
	workerBuffer        int = 16 //buffer size of requests PER WORKER
)

const ( //config names (remember that a '-' in the config file becomes a '_' in the name
	regexConfigName            = `Regex`
	regexExtractHostName       = `Regex_Extraction_Host`
	regexExtractRecordTypeName = `Regex_Extraction_Record_Type`
	dnsServerConfigName        = `DNS_Server`
	appendFormatConfigName     = `Append_Format`
	timeoutConfigName          = `Timeout`
	workerCountName            = `Worker_Count`
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

	ErrNotReady = errors.New("not ready")
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
	Synchronous               bool
	re                        *regexp.Regexp
	hostIdx                   int
	recordIdx                 int
}

// The Config function
func Config(cm gravwell.ConfigMap, tgr gravwell.Tagger) (err error) {
	//mtx.Lock()
	//defer mtx.Unlock()
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
	} else if cfg.WorkerCount < 0 {
		return fmt.Errorf("invalid Worker-Count %d", cfg.WorkerCount)
	} else if len(cfg.DNSServer) == 0 {
		return fmt.Errorf("missing DNS-Server configuration, please specify at least one DNS-Server")
	} else {
		debug("Regex %q with lookup on %s\n", cfg.Regex, cfg.RegexExtractionHost)
	}
	if cfg.WorkerCount == 0 {
		cfg.WorkerCount = defaultWorkerCount
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
	return
}

func Close() (err error) {
	return
}

func Flush() []*entry.Entry {
	return nil //we don't hold on to anything
}

func Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	if !ready {
		return nil, ErrNotReady
	}
	for i := range ents {
		ents[i].Data = enrich(ents[i].Data)
	}
	return ents, nil
}

func enrich(v []byte) (r []byte) {
	/*
		r = v // incase the lookup fails
		var name string
		var network string
		if subs := cfg.re.FindSubmatch(v); subs == nil || len(subs) <= cfg.hostIdx || len(subs) <= cfg.recordIdx {
			debug("Regex failed on: %q\n", string(v))
			return
		} else {
			name = string(subs[cfg.hostIdx])
			if rt := string(subs[cfg.recordIdx]); rt == `A` {
				network = `ip4`
			} else if rt == `AAAA` {
				network = `ip6`
			} else {
				network = `ip` //uuuh, best guess
			}
		}
	*/
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
	host string
	aaaa bool
}

type response struct {
	host string
	ips  []net.IP
}

func useWorker(nsaddr string, qnames []string) {
	ctx := context.Background()
	wg, err := newWorkerGroup([]string{nsaddr}, int(cfg.WorkerCount), workerBuffer, cfg.Timeout, ctx)
	if err != nil {
		fmt.Println("Failed to start worker group", err)
		return
	}
	if err := startWorkerGroup(wg); err != nil {
		fmt.Println("Failed to start worker group", err)
		return
	}

	for _, qname := range qnames {
		addWorkerGroupJob(wg, qname, false)
	}
	if err = stopWorkerGroup(wg); err != nil {
		fmt.Println("Failed to stop worker group")
		return
	}
	set := workerGroupConsumeReadySet(wg)
	fmt.Println("got", len(set))
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
		mtx:     &sync.Mutex{},
		wg:      &sync.WaitGroup{},
		workers: workers,
		input:   make(chan request, cnt*wb),
		retry:   make(chan request, cnt),
		output:  make(chan response, cnt*wb),
	}
	return
}

func startWorkerGroup(wg *workerGroup) error {
	wg.mtx.Lock()
	defer wg.mtx.Unlock()
	if wg.started {
		return errors.New("already started")
	}
	wg.started = true
	for i := range wg.workers {
		wg.workers[i].wg.Add(1)
		go workerRoutine(wg.workers[i], wg.retry, wg.input, wg.output)
	}
	wg.wg.Add(1)
	go workerGroupResponseHandler(wg)
	wg.running = true
	return nil
}

func stopWorkerGroup(wg *workerGroup) error {
	wg.mtx.Lock()
	if !wg.started {
		wg.mtx.Unlock()
		return errors.New("not started")
	}
	wg.running = false
	close(wg.input)
	wg.mtx.Unlock() //unlock so that workers and consumer can unlock
	for _, w := range wg.workers {
		w.wg.Wait()
	}
	//take the first worker and make him finish out all the retries
	w := wg.workers[0]
	for len(wg.retry) > 0 {
		r := <-wg.retry
		ips, _, _ := resolve(w, r.host)
		wg.output <- response{host: r.host, ips: ips}
	}
	wg.mtx.Lock() //relock to close out workers

	//close the workers
	for _, w := range wg.workers {
		closeWorker(w)
	}
	//close output
	close(wg.output)
	wg.mtx.Unlock()
	wg.wg.Wait()
	return nil
}

func addWorkerGroupJob(wg *workerGroup, host string, aaaa bool) (ok bool) {
	wg.mtx.Lock()
	if wg.started && wg.running {
		wg.input <- request{
			host: host,
			aaaa: aaaa,
		}
		ok = true
	}
	wg.mtx.Unlock()
	return
}

func workerGroupAppendReadySet(wg *workerGroup, set []response) {
	if len(set) == 0 {
		return
	}
	wg.mtx.Lock()
	wg.ready = append(wg.ready, set...)
	wg.mtx.Unlock()
}

func workerGroupConsumeReadySet(wg *workerGroup) (set []response) {
	wg.mtx.Lock()
	if len(wg.ready) > 0 {
		set = wg.ready
		wg.ready = nil
	}
	wg.mtx.Unlock()
	return
}

func workerGroupResponseHandler(wg *workerGroup) {
	defer wg.wg.Done()
	for r := range wg.output {
		set := []response{r}
		for len(wg.output) > 0 {
			if r, ok := <-wg.output; !ok {
				break
			} else {
				set = append(set, r)
			}
		}
		workerGroupAppendReadySet(wg, set)
	}
}

type workerGroup struct {
	mtx     *sync.Mutex
	wg      *sync.WaitGroup
	started bool
	running bool
	workers []*worker
	input   chan request
	retry   chan request
	output  chan response

	ready []response
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

func workerRoutine(w *worker, retry, in chan request, out chan response) {
	defer w.wg.Done()
	var processed int
	/* debugging
	defer func(v *int) {
		fmt.Println("Worker done", *v, w.c.Net)
	}(&processed)
	*/

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
		if ips, _, err := resolve(w, r.host); err != nil {
			//failed to resolve, put it back on the reque channel, wait 1 second and try again
			retry <- r
			time.Sleep(time.Second)
			continue
		} else {
			out <- response{
				host: r.host,
				ips:  ips,
			}
			processed++
		}
	}
	//try to handle the retries
	for {
		select {
		case r, ok := <-retry:
			if !ok {
				return
			} else if ips, _, err := resolve(w, r.host); err != nil {
				retry <- r //kick it back into the retry loop and bail
				fmt.Println(err)
				return
			} else {
				out <- response{
					host: r.host,
					ips:  ips,
				}
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
func exchange(w *worker, host string, canRetry bool) (r *dns.Msg, err error) {
	w.m.Question[0] = dns.Question{Name: dns.Fqdn(host), Qtype: dns.TypeA, Qclass: dns.ClassINET}
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
				r, err = exchange(w, host, false)
			}
		}
	} else if err = w.co.SetReadDeadline(time.Now().Add(w.to)); err != nil {
		return
	} else if r, err = w.co.ReadMsg(); err != nil {
		if canRetry {
			if lerr := reinitConn(w); lerr == nil {
				r, err = exchange(w, host, false)
			}
		}
	}
	return
}

func resolve(w *worker, host string) (ips []net.IP, ttl time.Duration, err error) {
	var r *dns.Msg
	w.mtx.Lock()
	defer w.mtx.Unlock()
	if w.co == nil {
		if err = reinitConn(w); err != nil {
			return
		}
	}
	if r, err = exchange(w, host, true); err != nil {
		return
	}
	//check if the response is Truncated, if so, reinit as a TCP connection and try again
	if r.Truncated && w.c.Net == `udp` {
		if err = reinitConn(w); err != nil {
			return
		} else if r, err = exchange(w, host, false); err != nil {
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
			}
		} else if a, ok := v.(*dns.AAAA); ok {
			if ip := a.AAAA; ip != nil {
				ips = dedupAppend(ips, ip)
			}
		} else if cn, ok := v.(*dns.CNAME); ok {
			if cn.Target != `` {
				if lips := recurseResolveRunner(cn.Hdr.Name, w.co, w.m, 0, nil); len(lips) > 0 {
					ips = dedupAppendSet(ips, lips)
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

func recurseResolve(nm string, co *dns.Conn, m *dns.Msg) []net.IP {
	return recurseResolveRunner(nm, co, m, 0, nil)
}

func recurseResolveRunner(nm string, co *dns.Conn, m *dns.Msg, depth int, inIPs []net.IP) (out []net.IP) {
	var r *dns.Msg
	var err error
	out = inIPs
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
				out = append(out, a.A)
			}
		case *dns.CNAME:
			if a.Target != `` {
				out = recurseResolveRunner(a.Target, co, m, depth+1, out)
			}
		}
	}
	return
}
