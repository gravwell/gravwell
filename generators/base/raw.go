/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package base

import (
	"errors"
	"net"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

type RawConn struct {
	dst  string
	conn net.Conn
	tags map[string]entry.EntryTag
	ip   net.IP
}

func newRawConn(gc GeneratorConfig, to time.Duration) (gConn GeneratorConn, err error) {
	if !gc.ok || !gc.modeRawTCP {
		return nil, errors.New("config is invalid")
	} else if gc.Raw == `` {
		return nil, errors.New("no connection endpoint specified")
	}
	return newRawConnType(gc, to, `tcp`)
}

func newRawUDPConn(gc GeneratorConfig, to time.Duration) (gConn GeneratorConn, err error) {
	if !gc.ok || !gc.modeRawUDP {
		return nil, errors.New("config is invalid")
	} else if gc.Raw == `` {
		return nil, errors.New("no connection endpoint specified")
	}
	return newRawConnType(gc, to, `udp`)
}

func newRawConnType(gc GeneratorConfig, to time.Duration, tp string) (gConn GeneratorConn, err error) {
	var conn net.Conn
	if conn, err = net.DialTimeout(tp, gc.Raw, to); err != nil {
		return
	}
	gConn = &RawConn{
		dst:  gc.Raw,
		conn: conn,
		tags: map[string]entry.EntryTag{},
		ip:   getIP(conn.LocalAddr().String()),
	}
	return
}

func (rc *RawConn) Close() (err error) {
	if rc == nil || rc.conn == nil {
		return errors.New("not open")
	}
	return rc.conn.Close()
}

func (rc *RawConn) GetTag(v string) (tag entry.EntryTag, err error) {
	tag, err = rc.NegotiateTag(v)
	return
}

func (rc *RawConn) NegotiateTag(v string) (tag entry.EntryTag, err error) {
	var ok bool
	if err = ingest.CheckTag(v); err != nil {
		return
	} else if tag, ok = rc.tags[v]; ok {
		return
	}
	tag = entry.EntryTag(len(rc.tags))
	rc.tags[v] = tag
	ok = true
	return
}

func (rc *RawConn) LookupTag(tag entry.EntryTag) (string, bool) {
	for k, v := range rc.tags {
		if v == tag {
			return k, true
		}
	}
	return ``, false
}

func (rc *RawConn) WaitForHot(time.Duration) error {
	return nil //always hot
}

func (rc *RawConn) Write(ts entry.Timestamp, tag entry.EntryTag, data []byte) error {
	return rc.writeBytes(data)
}

func (rc *RawConn) WriteBatch(ents []*entry.Entry) error {
	for _, ent := range ents {
		if err := rc.WriteEntry(ent); err != nil {
			return err
		}
	}
	return nil
}

func (rc *RawConn) WriteEntry(ent *entry.Entry) error {
	if ent == nil {
		return errors.New("nil entry")
	} else if rc == nil || rc.conn == nil {
		return errors.New("not ready")
	}
	return rc.writeBytes(ent.Data)
}

func (rc *RawConn) writeBytes(bts []byte) (err error) {
	var n int
	for len(bts) > 0 {
		if n, err = rc.conn.Write(bts); err != nil {
			return
		}
		bts = bts[n:]
	}
	_, err = rc.conn.Write([]byte("\n"))
	return
}

func (rc *RawConn) Sync(time.Duration) error {
	return nil
}

func (rc *RawConn) SourceIP() (net.IP, error) {
	if rc == nil || rc.ip == nil {
		return nil, errors.New("failed to get IP")
	}
	return rc.ip, nil
}

func getIP(s string) net.IP {
	if host, _, err := net.SplitHostPort(s); err == nil {
		return net.ParseIP(host)
	}
	return net.ParseIP(`192.168.1.1`)
}
