/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/binary"
	"math"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/netflow"
	"github.com/gravwell/gravwell/v3/timegrinder"
	"github.com/stretchr/testify/require"
)

const (
	testEntryTimeout = 2 * time.Second
	// testNowSlop is how far from the wall clock an entry stamped "now" may be
	testNowSlop = time.Minute

	nfv9Version  = 9
	ipfixVersion = 10
)

// headerTimeCase is a header export time and whether the entry should keep it
type headerTimeCase struct {
	name string
	sec  uint32
	keep bool
}

func headerTimeCases() []headerTimeCase {
	now := time.Now()
	return []headerTimeCase{
		{name: "current", sec: uint32(now.Unix()), keep: true},
		{name: "seven hours behind", sec: uint32(now.Add(-7 * time.Hour).Unix()), keep: true},
		{name: "half an hour ahead", sec: uint32(now.Add(30 * time.Minute).Unix()), keep: true},
		{name: "nine hours behind", sec: uint32(now.Add(-9 * time.Hour).Unix())},
		{name: "two hours ahead", sec: uint32(now.Add(2 * time.Hour).Unix())},
		{name: "ten years ahead", sec: uint32(now.AddDate(10, 0, 0).Unix())},
		{name: "max uint32", sec: math.MaxUint32},
		{name: "unix epoch", sec: 0},
	}
}

func newTestBindConfig() bindConfig {
	return bindConfig{
		ch:       make(chan *entry.Entry, 4),
		wg:       &sync.WaitGroup{},
		igst:     &ingest.IngestMuxer{}, // handlers only nil check the muxer
		tsWindow: defaultHeaderTimeWindow,
	}
}

// startTestHandler listens on loopback and starts the handler, it is closed when the test ends
func startTestHandler(t *testing.T, bh BindHandler, bc bindConfig) {
	t.Helper()
	lg = log.NewDiscardLogger()
	require.NoError(t, bh.Listen("127.0.0.1:0"))
	bc.wg.Add(1)
	require.NoError(t, bh.Start(1))
	t.Cleanup(func() {
		bh.Close()
		bc.wg.Wait()
	})
}

// sendDatagram sends pkt to the handler socket and returns the entry it produced
func sendDatagram(t *testing.T, to net.Addr, ch chan *entry.Entry, pkt []byte) *entry.Entry {
	t.Helper()
	c, err := net.Dial("udp", to.String())
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Write(pkt)
	require.NoError(t, err)
	select {
	case e := <-ch:
		require.NotNil(t, e)
		return e
	case <-time.After(testEntryTimeout):
		t.Fatal("timed out waiting for an entry")
	}
	return nil
}

func sendV5(t *testing.T, bc bindConfig, sec, nsec uint32) *entry.Entry {
	t.Helper()
	bc.wg = &sync.WaitGroup{} // one per handler so cleanup only waits on its own routine
	h, err := NewNetflowV5Handler(bc)
	require.NoError(t, err)
	startTestHandler(t, h, bc)
	hdr := netflow.NFv5Header{Version: 5, Count: 1, Sec: sec, Nsec: nsec}
	pkt := append(hdr.Encode(), make([]byte, netflow.RecordSize)...)
	return sendDatagram(t, h.c.LocalAddr(), bc.ch, pkt)
}

// sendIpfix sends a header only IPFIX or Netflow v9 message
func sendIpfix(t *testing.T, bc bindConfig, version uint16, exportTime uint32) *entry.Entry {
	t.Helper()
	bc.wg = &sync.WaitGroup{} // one per handler so cleanup only waits on its own routine
	h, err := NewIpfixHandler(bc)
	require.NoError(t, err)
	startTestHandler(t, h, bc)
	var pkt []byte
	switch version {
	case nfv9Version:
		pkt = make([]byte, 20)
		binary.BigEndian.PutUint32(pkt[8:], exportTime)
	case ipfixVersion:
		pkt = make([]byte, 16)
		binary.BigEndian.PutUint16(pkt[2:], uint16(len(pkt)))
		binary.BigEndian.PutUint32(pkt[4:], exportTime)
	default:
		t.Fatalf("unknown flow version %d", version)
	}
	binary.BigEndian.PutUint16(pkt, version)
	return sendDatagram(t, h.c.LocalAddr(), bc.ch, pkt)
}

func requireStampedNow(t *testing.T, ts entry.Timestamp) {
	t.Helper()
	require.WithinDuration(t, time.Now(), ts.StandardTime(), testNowSlop)
}

func requireHeaderTime(t *testing.T, keep bool, sec uint32, ts entry.Timestamp) {
	t.Helper()
	if keep {
		require.Equal(t, entry.UnixTime(int64(sec), 0), ts)
	} else {
		requireStampedNow(t, ts)
	}
}

var flowVersions = map[string]uint16{"ipfix": ipfixVersion, "netflow v9": nfv9Version}

func TestNetflowV5HeaderTime(t *testing.T) {
	for _, tc := range headerTimeCases() {
		t.Run(tc.name, func(t *testing.T) {
			requireHeaderTime(t, tc.keep, tc.sec, sendV5(t, newTestBindConfig(), tc.sec, 0).TS)
		})
	}
}

func TestNetflowV5HeaderNanoseconds(t *testing.T) {
	sec := uint32(time.Now().Unix())
	t.Run("in range", func(t *testing.T) {
		nsec := uint32(time.Second - 1)
		require.Equal(t, entry.UnixTime(int64(sec), int64(nsec)), sendV5(t, newTestBindConfig(), sec, nsec).TS)
	})
	for name, nsec := range map[string]uint32{"one second": uint32(time.Second), "max uint32": math.MaxUint32} {
		t.Run(name, func(t *testing.T) {
			ts := sendV5(t, newTestBindConfig(), sec, nsec).TS
			require.Less(t, ts.Nsec, int64(time.Second))
			requireStampedNow(t, ts)
		})
	}
}

func TestIpfixHeaderTime(t *testing.T) {
	for name, version := range flowVersions {
		for _, tc := range headerTimeCases() {
			t.Run(name+" "+tc.name, func(t *testing.T) {
				requireHeaderTime(t, tc.keep, tc.sec, sendIpfix(t, newTestBindConfig(), version, tc.sec).TS)
			})
		}
	}
}

func TestIgnoreTimestamps(t *testing.T) {
	bc := newTestBindConfig()
	bc.ignoreTS = true
	bc.disableTSControl = true // ignoring timestamps wins
	requireStampedNow(t, sendV5(t, bc, math.MaxUint32, math.MaxUint32).TS)
	for _, version := range flowVersions {
		requireStampedNow(t, sendIpfix(t, bc, version, math.MaxUint32).TS)
	}
}

// with timestamp control disabled every header time is kept as is
func TestDisableTimestampControl(t *testing.T) {
	bc := newTestBindConfig()
	bc.disableTSControl = true
	for _, tc := range headerTimeCases() {
		t.Run(tc.name, func(t *testing.T) {
			requireHeaderTime(t, true, tc.sec, sendV5(t, bc, tc.sec, 0).TS)
			for _, version := range flowVersions {
				requireHeaderTime(t, true, tc.sec, sendIpfix(t, bc, version, tc.sec).TS)
			}
		})
	}
	want := entry.UnixTime(math.MaxUint32, math.MaxUint32)
	require.Equal(t, want, sendV5(t, bc, math.MaxUint32, math.MaxUint32).TS)
}

// a window other than the default is honored by the handlers
func TestCustomWindow(t *testing.T) {
	bc := newTestBindConfig()
	bc.tsWindow = timegrinder.TimestampWindow{MaxPastDelta: 48 * time.Hour, MaxFutureDelta: time.Minute}
	now := time.Now()
	tests := []headerTimeCase{
		{name: "a day behind", sec: uint32(now.Add(-24 * time.Hour).Unix()), keep: true},
		{name: "three days behind", sec: uint32(now.Add(-72 * time.Hour).Unix())},
		{name: "half an hour ahead", sec: uint32(now.Add(30 * time.Minute).Unix())},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requireHeaderTime(t, tc.keep, tc.sec, sendV5(t, bc, tc.sec, 0).TS)
			for _, version := range flowVersions {
				requireHeaderTime(t, tc.keep, tc.sec, sendIpfix(t, bc, version, tc.sec).TS)
			}
		})
	}
}

const testConfigHeader = `[Global]
	Ingest-Secret = "IngestSecrets"
	Cleartext-Backend-Target=127.0.0.1:4023
`

const testConfigCollectors = `
[Collector "v5"]
	Bind-String="127.0.0.1:2055"
[Collector "ipfix"]
	Bind-String="127.0.0.1:4739"
	Flow-Type=ipfix
	Disable-Timestamp-Control=true
`

func loadTestConfig(t *testing.T, global string) (*cfgType, error) {
	t.Helper()
	pth := filepath.Join(t.TempDir(), "netflow_capture.conf")
	require.NoError(t, os.WriteFile(pth, []byte(testConfigHeader+global+testConfigCollectors), 0600))
	return GetConfig(pth, ``)
}

func TestConfigDisableTimestampControl(t *testing.T) {
	cfg, err := loadTestConfig(t, ``)
	require.NoError(t, err)
	require.False(t, cfg.Collector["v5"].Disable_Timestamp_Control)
	require.True(t, cfg.Collector["ipfix"].Disable_Timestamp_Control)
}

func TestConfigHeaderTimeWindow(t *testing.T) {
	def := defaultHeaderTimeWindow
	require.Equal(t, 8*time.Hour, def.MaxPastDelta)
	require.Equal(t, time.Hour, def.MaxFutureDelta)
	tests := []struct {
		name   string
		global string
		want   timegrinder.TimestampWindow
	}{
		{name: "unset", want: def},
		{
			name:   "both set",
			global: "Timestamp-Max-Past-Delta=72h\nTimestamp-Max-Future-Delta=10m\n",
			want:   timegrinder.TimestampWindow{MaxPastDelta: 72 * time.Hour, MaxFutureDelta: 10 * time.Minute},
		},
		{
			name:   "past only",
			global: "Timestamp-Max-Past-Delta=72h\n",
			want:   timegrinder.TimestampWindow{MaxPastDelta: 72 * time.Hour, MaxFutureDelta: def.MaxFutureDelta},
		},
		{
			name:   "future only",
			global: "Timestamp-Max-Future-Delta=10m\n",
			want:   timegrinder.TimestampWindow{MaxPastDelta: def.MaxPastDelta, MaxFutureDelta: 10 * time.Minute},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadTestConfig(t, tc.global)
			require.NoError(t, err)
			w, err := cfg.headerTimeWindow()
			require.NoError(t, err)
			require.Equal(t, tc.want, w)
		})
	}
	_, err := loadTestConfig(t, "Timestamp-Max-Past-Delta=notaduration\n")
	require.Error(t, err)
}
