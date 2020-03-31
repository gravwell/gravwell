/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/ingest/v3/config"
	"github.com/gravwell/ingest/v3/entry"
)

const (
	ForwarderProcessor string = `forwarder`

	protoTCP  string = `tcp`
	protoUDP  string = `udp`
	protoTLS  string = `tls`
	protoUnix string = `unix`

	defaultProto string = protoTCP

	defaultFormat string = `raw`

	defaultDelimiter = "\n"

	defaultBuffer uint = 256

	redialInterval = time.Second
)

var (
	ErrNoUnixOnWindows = errors.New("Unix transport not available on Windows")
	ErrMissingTarget   = errors.New("Target IP:Port or Unix path required")
	ErrUnknownProtocol = errors.New("Unknown protocol")
	ErrUnknownFormat   = errors.New("Unknown format")
	ErrClosed          = errors.New("Closed")
	ErrNilTagger       = errors.New("invalid parameter, missing tagger")
)

type ForwarderConfig struct {
	Target                   string
	Protocol                 string
	Delimiter                string
	Format                   string
	Timeout                  uint //timeout in seconds for a write
	Buffer                   uint //number of entries in flight (basically channel buffer size)
	Non_Blocking             bool
	Insecure_Skip_TLS_Verify bool
}

func ForwarderLoadConfig(vc *config.VariableConfig) (c ForwarderConfig, err error) {
	if err = vc.MapTo(&c); err != nil {
		return
	}
	err = c.Validate()
	return
}

type Forwarder struct {
	ForwarderConfig
	sync.Mutex
	tgr  Tagger
	wg   sync.WaitGroup
	ctx  context.Context
	cf   context.CancelFunc
	ch   chan *entry.Entry
	abrt chan struct{} //used to abort blocked writes
	conn net.Conn
	enc  EntryEncoder
	err  error
}

func NewForwarder(cfg ForwarderConfig, tgr Tagger) (nf *Forwarder, err error) {
	var conn net.Conn
	if err = cfg.Validate(); err != nil {
		return
	}
	if tgr == nil {
		err = ErrNilTagger
		return
	}
	nf = &Forwarder{
		ForwarderConfig: cfg,
		ch:              make(chan *entry.Entry, cfg.Buffer),
		abrt:            make(chan struct{}),
		tgr:             tgr,
	}
	nf.ctx, nf.cf = context.WithCancel(context.Background())
	if !nf.Non_Blocking {
		if conn, err = nf.newConnection(false); err != nil {
			return
		}
	}
	nf.wg.Add(1)
	go nf.routine(conn)
	return
}

func (nf *Forwarder) Process(ent *entry.Entry) (r []*entry.Entry, err error) {
	if nf.Non_Blocking {
		r, err = nf.nonblockingProcess(ent)
	} else {
		r, err = nf.blockingProcess(ent)
	}
	return
}

func (nf *Forwarder) blockingProcess(ent *entry.Entry) (r []*entry.Entry, err error) {
	select {
	case <-nf.abrt: //aborted on close
	case nf.ch <- ent:
		r = []*entry.Entry{ent}
	}
	return
}

func (nf *Forwarder) nonblockingProcess(ent *entry.Entry) (r []*entry.Entry, err error) {
	select {
	case nf.ch <- ent:
		r = []*entry.Entry{ent}
	default: //if we can't write, sorry, ROLL ON!
	}
	return
}

func (nf *Forwarder) Close() (err error) {
	close(nf.abrt)
	nf.Lock()
	//close the channel
	close(nf.ch)
	defer nf.Unlock()
	//wait for up to timeout for the routine to exit
	nf.wait(nf.Timeout)
	//if we hit here we KNOW the routine exited
	err = nf.err
	return
}

// wait for the waitgroup with a timeout
func (nf *Forwarder) wait(tosec uint) {
	var to time.Duration
	if tosec == 0 {
		tosec = 1
	}
	to = time.Duration(tosec) * time.Second

	ch := make(chan bool, 1)
	go func(wg *sync.WaitGroup, done chan bool) {
		wg.Wait()
		close(done)
	}(&nf.wg, ch)

	select {
	case <-time.After(to):
		nf.cf() //cancel the context and wait
		if nf.conn != nil {
			nf.conn.Close() //incase they are blocked on a write
		}
		<-ch
	case <-ch:
	}
	return
}

func (nf *Forwarder) routine(conn net.Conn) {
	defer nf.wg.Done()
	//grab a connetion if we were not handed one
	if conn == nil {
		if conn, nf.err = nf.newConnection(true); nf.err != nil {
			return
		}
	}

	if nf.err = nf.newEncoder(conn); nf.err != nil {
		return
	}

	for ent, ok := nf.getEnt(); ok == true; ent, ok = nf.getEnt() {
		if conn, nf.err = nf.sendEntry(ent, conn); nf.err != nil {
			break
		}
	}
}

func (nf *Forwarder) getEnt() (ent *entry.Entry, ok bool) {
	select {
	case ent, ok = <-nf.ch:
	case <-nf.ctx.Done():
	}
	return
}

func (nf *Forwarder) sendEntry(ent *entry.Entry, conn net.Conn) (nc net.Conn, err error) {
	nc = conn
	if ent == nil {
		return //skip
	}

	for {
		if err = nf.enc.Encode(ent); err == nil || err == context.Canceled {
			break //all good or cancelled context
		}
		//failed to send it, try to get a new connection
		nc.Close()
		if nc, err = nf.newConnection(true); err != nil {
			break //something failed, bail
		}
		//got a new connection, reset the encoder and roll on
		nf.enc.Reset(nc)
		nf.Lock()
		nf.conn = nc
		nf.Unlock()
	}
	return
}

func (nfc *ForwarderConfig) Validate() (err error) {
	//check variables and populate with defaults where needed
	if nfc.Target == `` {
		err = ErrMissingTarget
		return
	}
	if nfc.Protocol == `` {
		nfc.Protocol = defaultProto
	}
	if nfc.Format == `` {
		nfc.Format = defaultFormat
	} else {
		nfc.Format = strings.ToLower(strings.TrimSpace(nfc.Format))
	}

	if nfc.Format == encRaw && nfc.Delimiter == `` {
		nfc.Delimiter = defaultDelimiter
	}
	if nfc.Buffer == 0 {
		nfc.Buffer = defaultBuffer
	}
	nfc.Protocol = strings.ToLower(nfc.Protocol)

	//check the Protocol against the what was specified in the target
	//check that the protocol is valid
	switch nfc.Protocol {
	case protoUnix:
		var fi os.FileInfo
		//check that we are not on windows
		if runtime.GOOS == `windows` {
			err = ErrNoUnixOnWindows
			return
		}

		//target better be a valid path to a socket
		if fi, err = os.Stat(nfc.Target); err != nil {
			if os.IsNotExist(err) {
				err = fmt.Errorf("%s is not a valid Unix named socket", nfc.Target)
			}
			return //some other error
		}
		//check that the stated path is a socket
		if (fi.Mode() & os.ModeType) != os.ModeSocket {
			err = fmt.Errorf("Path %s does not point to a Unix Named socket", nfc.Target)
			return
		}
		//all good
	case protoTCP:
		fallthrough
	case protoUDP:
		fallthrough
	case protoTLS:
		var h string
		if h, _, err = net.SplitHostPort(nfc.Target); err != nil {
			return
		}
		//try to resolve the host
		if _, err = net.ResolveIPAddr(`ip`, h); err != nil {
			err = fmt.Errorf("Unable to resolve host %s: %v", h, err)
			return
		}
	default: //everything else better be a host:port pair
		err = ErrUnknownProtocol
		return
	}
	return
}

func (nfc *Forwarder) newConnection(retry bool) (conn net.Conn, err error) {
	var d net.Dialer
	for retry {
		switch nfc.Protocol {
		case protoTCP:
			conn, err = d.DialContext(nfc.ctx, `tcp`, nfc.Target)
		case protoUDP:
			conn, err = d.DialContext(nfc.ctx, `udp`, nfc.Target)
		case protoUnix:
			conn, err = d.DialContext(nfc.ctx, `unix`, nfc.Target)
		case protoTLS:
			cfg := tls.Config{
				InsecureSkipVerify: nfc.Insecure_Skip_TLS_Verify,
			}
			conn, err = tls.DialWithDialer(&d, `tcp`, nfc.Target, &cfg)
		}
		if err == context.Canceled {
			retry = false
		}
		if err == nil {
			break
		}
		//redial interval is 1 second
		if nfc.sleep(redialInterval) {
			//sleep was cancelled
			err = context.Canceled
			break
		}
	}
	return
}

func (nfc *Forwarder) newEncoder(w io.Writer) (err error) {
	switch nfc.Format {
	case encRaw:
		//we do this so that we can pass in a nil if its empty
		var b []byte
		if nfc.Delimiter != `` {
			b = []byte(nfc.Delimiter)
		}
		nfc.enc, err = newRawEncoder(w, b)
	case encJSON:
		nfc.enc, err = newJSONEncoder(w, nfc.tgr)
	case encSYSLOG:
		nfc.enc, err = newSyslogEncoder(w, nfc.tgr)
	default:
		err = ErrUnknownFormat
	}
	return
}

func (nfc *Forwarder) dialer() (d *net.Dialer) {
	if nfc.Timeout > 0 {
		d.Timeout = time.Duration(nfc.Timeout) * time.Second
	}
	return
}

func (nfc *Forwarder) sleep(d time.Duration) (cancelled bool) {
	select {
	case <-nfc.ctx.Done():
		cancelled = true
	case <-time.After(d):
	}
	return
}
