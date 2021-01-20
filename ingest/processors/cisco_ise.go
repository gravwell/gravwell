/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gobwas/glob"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var (
	ErrInvalidRemoteISEHeader = errors.New("Failed to match remote ISE header")
	ErrInvalidISEHeader       = errors.New("Failed to match ISE header")
	ErrInvalidRemoteISESeq    = errors.New("Invalid multipart message sequence")
	ErrInvalidISESeq          = errors.New("Invalid ISE message sequence")
)

const (
	CiscoISEProcessor string = `cisco_ise`

	outputRaw  string = `raw`
	outputCEF  string = `cef`
	outputJSON string = `json`

	defaultMultipartMaxBuffer = 8 * 1024 * 1024 //8MB, which is ALOT

	iseTimestampFormat string = `2006-01-02 15:04:05.999999999 -07:00`

	iseRaw  = 0
	iseCef  = 1
	iseJSON = 2
)

type CiscoISEConfig struct {
	Passthrough_Misses          bool
	Enable_Multipart_Reassembly bool
	Max_Multipart_Buffer        uint64
	Max_Multipart_Latency       string
	Output_Format               string
	Attribute_Drop_Filter       []string
	Attribute_Strip_Header      bool
	maxLatency                  time.Duration
	format                      uint
	filters                     []glob.Glob
}

func CiscoISELoadConfig(vc *config.VariableConfig) (c CiscoISEConfig, err error) {
	if err = vc.MapTo(&c); err != nil {
		return
	}
	err = c.validate()
	return
}

func (c *CiscoISEConfig) validate() (err error) {
	if c.Max_Multipart_Latency != `` {
		if c.maxLatency, err = time.ParseDuration(c.Max_Multipart_Latency); err != nil {
			if err = fmt.Errorf("Invalid Max-Multipart-Latency %q: %v", c.Max_Multipart_Latency, err); err != nil {
				return
			}
		}
	}

	//check if an output format has been specified and validate it
	switch strings.ToLower(c.Output_Format) {
	case outputJSON:
		c.format = iseJSON
	case outputCEF:
		c.format = iseCef
	case ``: //default is raw
	case outputRaw:
		c.format = iseRaw
		//ensure there are not any filters specified
		if len(c.Attribute_Drop_Filter) > 0 {
			err = fmt.Errorf("The %s Output-Format is not compatible with Attribute-Drop-Filters", c.Output_Format)
		} else if c.Attribute_Strip_Header {
			err = fmt.Errorf("The %s Output-Format is not compatible with Attribute-Strip-Header", c.Output_Format)
		}
	default:
		err = fmt.Errorf("Unknown output format %q", c.Output_Format)
	}

	//compile our globs
	var filter glob.Glob
	for _, f := range c.Attribute_Drop_Filter {
		if filter, err = glob.Compile(f); err != nil {
			err = fmt.Errorf("Invalid filter %s: %w", f, err)
			return
		}
		c.filters = append(c.filters, filter)
	}

	return
}

// formatter functions
type iseFormatter func(*entry.Entry, []glob.Glob, bool) bool

type CiscoISE struct {
	CiscoISEConfig
	fmt iseFormatter
	ma  *multipartAssembler
}

func NewCiscoISEProcessor(cfg CiscoISEConfig) (ise *CiscoISE, err error) {
	if err = cfg.validate(); err != nil {
		return
	}
	var f iseFormatter
	switch cfg.format {
	case iseJSON:
		f = iseJSONFormatter
	case iseRaw:
		f = iseRawFormatter
	case iseCef:
		f = iseCefFormatter
	default:
		err = fmt.Errorf("invalid formatter id %d", cfg.format)
		return
	}
	//check if we are re-assembling
	ise = &CiscoISE{
		CiscoISEConfig: cfg,
		fmt:            f,
	}
	if cfg.Enable_Multipart_Reassembly {
		ise.ma = newMultipartAssembler(cfg.Max_Multipart_Buffer, cfg.maxLatency)
	}

	return
}

func (p *CiscoISE) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(CiscoISEConfig); ok {
		p.CiscoISEConfig = cfg
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (p *CiscoISE) Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	if len(ents) == 0 {
		return nil, nil
	}
	rset := ents[:0]
	for _, v := range ents {
		if ent, err := p.processEnt(v); ent != nil && err == nil {
			rset = append(rset, ent)
		}
	}

	//got a potential value, see if we have any that need to be ejected due to size or time
	if p.Enable_Multipart_Reassembly && p.ma.shouldFlush() {
		if ents := p.flush(false); len(ents) > 0 {
			rset = append(rset, ents...)
		}
	}

	return rset, nil
}

func (p *CiscoISE) processEnt(ent *entry.Entry) (r *entry.Entry, err error) {
	if p.Enable_Multipart_Reassembly {
		r, err = p.processReassemble(ent)
	} else {
		//just attempt to reformat the entry
		if !p.fmt(ent, p.filters, p.Attribute_Strip_Header) && !p.Passthrough_Misses {
			//bad formatting and no passthrough, just skip it
			return
		}
		r = ent
	}
	return
}

func (p *CiscoISE) processReassemble(ent *entry.Entry) (r *entry.Entry, err error) {
	//add the item to our re-assembler
	var rmsg remoteISE
	if err = rmsg.Parse(string(ent.Data)); err != nil {
		err = nil // do not pass parsing errors up
		if p.Passthrough_Misses {
			r = ent
		}
	} else if msr, ejected, bad := p.ma.add(rmsg, ent); bad {
		if p.Passthrough_Misses {
			r = ent
		}
	} else if ejected {
		if rent, ok := msr.meta.(*entry.Entry); ok {
			rent.Data = []byte(msr.output)
			if p.fmt(rent, p.filters, p.Attribute_Strip_Header) || p.Passthrough_Misses {
				r = rent
			}
		}
	}

	return
}

func (p *CiscoISE) flush(force bool) (ents []*entry.Entry) {
	if outputs := p.ma.flush(force); len(outputs) > 0 {
		for _, out := range outputs {
			if rent, ok := out.meta.(*entry.Entry); ok {
				rent.Data = []byte(out.output)
				if p.fmt(rent, p.filters, p.Attribute_Strip_Header) || p.Passthrough_Misses {
					ents = append(ents, rent)
				}
			}
		}
	}

	return
}

func (p *CiscoISE) Flush() []*entry.Entry {
	return p.flush(true)
}

func (p *CiscoISE) Close() (err error) {
	return nil
}

type multipartAssembler struct {
	max        uint64 //maximum buffer size
	total      uint64 //total number of bytes in the buffer
	maxLatency time.Duration
	oldest     time.Time
	tracker    map[remoteISEHeaderSource]*messageSequence
}

func newMultipartAssembler(maxBuff uint64, maxLatency time.Duration) *multipartAssembler {
	if maxBuff == 0 {
		maxBuff = defaultMultipartMaxBuffer
	}
	//just make sure its sane
	if maxLatency <= 0 {
		maxLatency = 0
	}
	return &multipartAssembler{
		max:        maxBuff,
		tracker:    map[remoteISEHeaderSource](*messageSequence){},
		maxLatency: maxLatency,
		oldest:     time.Now(), //just initialize to now
	}
}

func (ma *multipartAssembler) add(msg remoteISE, meta interface{}) (msr messageSequenceResult, ejected, bad bool) {
	src := msg.remoteISEHeaderSource
	//check if we have an existing message
	if v, ok := ma.tracker[src]; ok {
		sz := v.size
		//v is a pointer, don't need to assign back in
		if ejected, bad = v.add(msg); bad {
			//something is wrong
			return
		} else if ejected {
			//this is the final message, eject it
			msr = v.finalize()
			delete(ma.tracker, src)
			ma.total -= sz
		} else {
			ma.total += uint64(len(msg.body)) //add in the size
		}
	} else {
		if msi, ok := newMessageSequence(msg, meta); !ok {
			bad = true
			return
		} else {
			ma.tracker[src] = msi
			ma.total += msi.size
		}
	}
	return
}

func (ma *multipartAssembler) shouldFlush() bool {
	if ma.total >= ma.max {
		return true
	} else if ma.maxLatency > 0 {
		if time.Since(ma.oldest) > ma.maxLatency {
			return true //there is something that needs to be purged
		}
	}
	return false
}

func (ma *multipartAssembler) flush(force bool) (outputs []messageSequenceResult) {
	var cutoff time.Time
	var checkTime bool
	if ma.maxLatency > 0 {
		checkTime = true
		cutoff = time.Now().Add(-1 * ma.maxLatency)
	}

	var oldest time.Time
	var oldestkey remoteISEHeaderSource
	for k, v := range ma.tracker {
		if force || (checkTime && v.last.Before(cutoff)) {
			outputs = append(outputs, v.finalize())
			delete(ma.tracker, k)
			ma.total -= v.size
		} else if oldest.IsZero() || v.last.Before(oldest) {
			oldest = v.last
			oldestkey = k
		}
	}
	if oldest.IsZero() {
		oldest = time.Now()
	}
	ma.oldest = oldest
	if len(ma.tracker) == 0 {
		ma.total = 0 //just to make sure we can reset properly
	}

	if ma.total > ma.max {
		// we NEED to eject something, so eject the oldest
		if v, ok := ma.tracker[oldestkey]; ok {
			outputs = append(outputs, v.finalize())
			delete(ma.tracker, oldestkey)
			ma.total -= v.size
		}
	}
	return
}

type messageSequenceResult struct {
	output string
	meta   interface{}
}

type messageSequence struct {
	remoteISEHeaderSource
	size   uint64
	total  uint16
	curr   uint16
	bodies []string
	last   time.Time
	meta   interface{}
}

// newMessageSequence generates a new message sequence from a given remoteISE
func newMessageSequence(msg remoteISE, meta interface{}) (ms *messageSequence, ok bool) {
	if msg.total == 0 || msg.seq >= msg.total {
		return // this is bad mmmkay
	}
	bodies := make([]string, msg.total)
	bodies[msg.seq] = msg.body
	ms = &messageSequence{
		remoteISEHeaderSource: msg.remoteISEHeaderSource,
		total:                 msg.total,
		curr:                  1, //we got the first one
		size:                  uint64(len(msg.body) + len(msg.host) + len(msg.cat)),
		bodies:                bodies,
		last:                  time.Now(),
		meta:                  meta,
	}
	ok = true
	return
}

// add takes in a remoteISE message and adds it to the tracked set
// we return done==true if this is the final message and this is ready to be finalized
// if we get a remote message with an ID that doesn't make sense, we return bad==true
func (ms *messageSequence) add(msg remoteISE) (done, bad bool) {
	if msg.seq >= uint16(len(ms.bodies)) {
		//bad index, just reject it
		bad = true
		return
	}
	ms.bodies[msg.seq] = msg.body
	ms.curr += 1
	ms.size += uint64(len(msg.body))
	if ms.curr == uint16(len(ms.bodies)) {
		//we got them all
		done = true
	} else {
		//update the hit
		ms.last = time.Now()
	}
	return
}

func (ms *messageSequence) finalize() (msr messageSequenceResult) {
	for i := range ms.bodies {
		msr.output += ms.bodies[i]
	}
	msr.meta = ms.meta
	return
}

var (
	//setup remote header extraction RX (see https://www.cisco.com/c/en/us/td/docs/security/ise/syslog/Cisco_ISE_Syslogs/m_IntrotoSyslogs.pdf page 4)
	remoteHeaderRx          = regexp.MustCompile(`^(?P<ts>\S+\s\d+\s\d+\:\d+\:\d+)(\s[-+]?\d+:\d+)?\s(?P<host>\S+)\s(?P<cat>\S+)\s(?P<msgid>\d+)\s(?P<total>\d+)\s(?P<seq>\d+)\s(?P<body>.+)$`)
	remoteHeaderRxParts int = 9
)

// remoteISEHeaderSource is the sub structure that represents a specific message category
type remoteISEHeaderSource struct {
	host string
	cat  string
	id   uint32
}

// remoteISEHeader is the greater structure representing a multipart message coming form a given host
type remoteISE struct {
	remoteISEHeaderSource
	total uint16
	seq   uint16
	body  string
}

// Parse will extract the remote ISE message header and body, we do NOT handle the timestamp here, it is discarded
func (rim *remoteISE) Parse(val string) (err error) {
	var v uint64
	r := remoteHeaderRx.FindStringSubmatch(val)
	if len(r) != (remoteHeaderRxParts) {
		err = ErrInvalidRemoteISEHeader
		return
	}
	//skip past the complete match and grab the string values, we are skipping the timestamp on multipart messages
	r = r[3:] //bits are 0:complete match 1:timestamp 2: optional timezone
	rim.host = r[0]
	rim.cat = r[1]
	//id uint32/3
	if v, err = strconv.ParseUint(r[2], 10, 32); err != nil {
		err = fmt.Errorf("%w %v", ErrInvalidRemoteISEHeader, err)
		return
	}
	rim.id = uint32(v)
	//total uint16/4
	if v, err = strconv.ParseUint(r[3], 10, 16); err != nil {
		err = fmt.Errorf("%w %v", ErrInvalidRemoteISEHeader, err)
		return
	}
	rim.total = uint16(v)
	//seq   uint16/5
	if v, err = strconv.ParseUint(r[4], 10, 16); err != nil {
		err = fmt.Errorf("%w %v", ErrInvalidRemoteISEHeader, err)
		return
	}
	rim.seq = uint16(v)
	rim.body = r[5]
	//double check the sequence and total for sanity
	if rim.seq > rim.total {
		err = fmt.Errorf("%w sequence %d > total %d", ErrInvalidRemoteISESeq, rim.seq, rim.total)
	}
	return
}

var (
	iseHeaderRx          = regexp.MustCompile(`^(?P<ts>\d+\-\d+\-\d+\s\d+\:\d+\:\d+(\.\d+)?(\s[-+]?\d+:\d+)?)\s(?P<seq>\d+)\s(?P<ode>\S+)\s(?P<sev>\S+)\s(?P<class>[^\:]+)\:\s(?P<body>.+)$`)
	iseHeaderRxParts int = 9
)

type iseMessage struct {
	ts    time.Time
	seq   uint32
	ode   string
	sev   string
	class string
	text  string
	attrs []iseKV
}

type iseKV struct {
	key   string
	value string
}

func (m *iseMessage) Parse(raw string, attribFilters []glob.Glob, stripHeaders bool) (err error) {
	var v uint64
	r := iseHeaderRx.FindStringSubmatch(raw)
	if len(r) != (iseHeaderRxParts) {
		err = ErrInvalidISEHeader
		return
	}
	//skip past the total match
	r = r[1:]

	//parse the timestamp
	if m.ts, err = time.Parse(iseTimestampFormat, r[0]); err != nil {
		err = fmt.Errorf("Invalid ISE timestamp %w", err)
		return
	}

	//skip past the timestamp chunks
	r = r[3:]

	//grab the seq
	if v, err = strconv.ParseUint(r[0], 10, 32); err != nil {
		err = fmt.Errorf("%w %v", ErrInvalidISEHeader, err)
		return
	}
	m.seq = uint32(v)

	//grab the ODE, severity, and class
	m.ode = r[1]
	m.sev = r[2]
	m.class = r[3]

	//get the body up and start cracking it
	body := []byte(r[4])

	//read the text message
	idx := indexOfNonEscaped(body, ',')
	if idx == -1 {
		//no attributes, assign text and bail
		m.text = r[4]
		return
	} else {
		m.text = string(body[:idx])
		body = body[idx+1:]
	}

	// start feeding on attributes
	for len(body) > 0 {
		var kv iseKV
		idx = indexOfNonEscaped(body, ',')
		if idx == -1 {
			//end of body, we are bailing one way or another
			if kv.parse(body, attribFilters, stripHeaders) {
				m.attrs = append(m.attrs, kv)
			}
			break
		}
		if kv.parse(body[:idx], attribFilters, stripHeaders) {
			m.attrs = append(m.attrs, kv)
		}
		body = body[idx+1:]
	}

	return
}

func (m *iseMessage) equal(n *iseMessage) bool {
	if m == n {
		return true
	} else if m == nil || n == nil {
		return false
	}
	if !m.ts.Equal(n.ts) {
		return false
	}
	if m.seq != n.seq || m.ode != n.ode || m.sev != n.sev || m.class != n.class || m.text != n.text {
		return false
	}
	if len(m.attrs) != len(n.attrs) {
		return false
	}
	for i, v := range m.attrs {
		if v != n.attrs[i] {
			return false
		}
	}
	return true
}

func (kv *iseKV) parse(val []byte, filters []glob.Glob, stripHeaders bool) bool {
	val = bytes.TrimSpace(val)
	//check if this is attribute is filtered
	if filtered(string(val), filters) {
		return false
	}
	if idx := indexOfNonEscaped(val, '='); idx == -1 {
		return false
	} else {
		kv.key = string(bytes.TrimSpace(val[0:idx]))
		kv.value = string(bytes.TrimSpace(val[idx+1:]))
	}
	if stripHeaders {
		kv.stripHeaders()
	}
	return true
}

func (kv *iseKV) stripHeaders() {
	//check if the value leads with any of our no-no characters
	if len(kv.value) == 0 {
		return //short circuit out
	}
	val := []byte(kv.value)
	key := kv.key
	if val[0] == '(' || val[0] == '{' {
		return //nope
	}
	//iterate in
	for idx := indexOfNonEscaped(val, '='); idx != -1 && idx != 0; idx = indexOfNonEscaped(val, '=') {
		key = string(val[:idx])
		val = val[idx+1:]
	}
	kv.key = key
	kv.value = string(val)
	return
}

func indexOfNonEscaped(data []byte, delim byte) (r int) {
	var idx int
	var offset int
	r = -1
	for {
		if idx = bytes.IndexByte(data[offset:], delim); idx < 0 {
			//never found it, just return
			break
		} else if idx == 0 {
			r = offset
			break
		}
		//ok, index is > 0 so check if its escaped
		if data[offset+idx-1] != '\\' {
			//not escaped, set r and break
			r = offset + idx
			break
		}
		//advance offset and continue, this is escaped
		offset += idx + 1 //advance past the comma
	}
	return
}

// iseRawFormatter is just a passthrough, send as is
func iseRawFormatter(ent *entry.Entry, filters []glob.Glob, stripHeaders bool) bool {
	return true
}

// iseCefFormatter attempts to crack the message apart and reform it as a CEF message
// this WILDLY violates the CEF spec, but so does everyone else
func iseCefFormatter(ent *entry.Entry, filters []glob.Glob, stripHeaders bool) bool {
	var msg iseMessage
	if err := msg.Parse(string(ent.Data), filters, stripHeaders); err != nil {
		return false
	}
	//update the timestamp
	ent.TS = entry.FromStandard(msg.ts)
	ent.Data = msg.formatAsCEF()
	return true
}

func (m *iseMessage) formatAsCEF() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "CEF:0|CISCO|ISE_DEVICE|||%s|%s| sequence=%d ode=%s class=%s text=%s",
		m.class, m.sev, m.seq, m.ode, m.class, m.text)
	for _, attr := range m.attrs {
		fmt.Fprintf(&b, " %s=%s",
			strings.ReplaceAll(attr.key, " ", ""),      //replace all spaces in keys
			strings.ReplaceAll(attr.value, "=", "\\=")) //replace all equal signs in values
	}
	return []byte(b.String())
}

// iseJSONFormatter attempts to crack the message apart and reform it as a JSON object
func iseJSONFormatter(ent *entry.Entry, filters []glob.Glob, stripHeaders bool) bool {
	var msg iseMessage
	if err := msg.Parse(string(ent.Data), filters, stripHeaders); err != nil {
		return false
	}
	if v, err := json.Marshal(msg); err == nil {
		ent.Data = v
		ent.TS = entry.FromStandard(msg.ts)
		return true
	}
	return false
}

func (m iseMessage) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		TS         time.Time
		Sequence   uint32
		ODE        string
		Severity   string
		Class      string
		Text       string
		Attributes kvAttrs
	}{
		TS:         m.ts,
		Sequence:   m.seq,
		ODE:        m.ode,
		Severity:   m.sev,
		Class:      m.class,
		Text:       m.text,
		Attributes: kvAttrs(m.attrs),
	})
}

type kvAttrs []iseKV

func (kv kvAttrs) MarshalJSON() ([]byte, error) {
	r := strings.NewReplacer(`\,`, `,`, `\\`, `\`, `\"`, `"`, `\'`, `'`)
	mp := make(map[string]string, len(kv))
	for _, v := range kv {
		mp[r.Replace(v.key)] = r.Replace(v.value)
	}
	return json.Marshal(mp)
}

func filtered(v string, filters []glob.Glob) bool {
	for _, f := range filters {
		if f.Match(v) {
			return true
		}
	}
	return false
}
