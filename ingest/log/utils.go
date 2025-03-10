/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package log

import (
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"

	"github.com/crewjam/rfc5424"
	"github.com/shirou/gopsutil/host"
)

func KV(name string, value interface{}) (r rfc5424.SDParam) {
	r.Name = name
	switch v := value.(type) {
	case string:
		r.Value = v
	default:
		r.Value = fmt.Sprintf("%v", value)
	}
	return
}

func KVErr(err error) rfc5424.SDParam {
	return KV("error", err)
}

func PrintOSInfo(wtr io.Writer) {
	if platform, _, version, err := host.PlatformInformation(); err == nil {
		fmt.Fprintf(wtr, "OS:\t\t%s %s [%s] (%s %s)\n", runtime.GOOS, runtime.GOARCH, kernelVersion, platform, version)
	} else {
		fmt.Fprintf(wtr, "OS:\t\tERROR %v\n", err)
	}
}

type udpRelay struct {
	conn net.PacketConn
	addr *net.UDPAddr
}

func (r *udpRelay) Write(b []byte) (n int, err error) {
	if len(b) == 1 && b[0] == '\n' {
		return 1, nil // don't send single newlines
	}
	n, err = r.conn.WriteTo(b, r.addr)
	return
}

func (r *udpRelay) Close() (err error) {
	if r == nil || r.conn == nil {
		return errors.New("not open")
	}
	return r.conn.Close()
}

func NewUdpRelay(tgt string) (*udpRelay, error) {
	var conn net.PacketConn
	var addr *net.UDPAddr
	var err error
	// Resolve the address and get the socket established
	if addr, err = net.ResolveUDPAddr("udp", tgt); err != nil {
		return nil, err
	} else if conn, err = net.ListenPacket("udp", ":0"); err != nil {
		return nil, err
	}
	return &udpRelay{
		conn: conn,
		addr: addr,
	}, nil
}

func NewUDPLogger(tgt string) (*Logger, error) {
	relay, err := NewUdpRelay(tgt)
	if err != nil {
		return nil, err
	}
	return New(relay), nil
}
