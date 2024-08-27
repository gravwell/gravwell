/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

//TODO TESTING - write some tests that screw up the ids and force a resend

package ingest

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/entry"
)

const (
	PIPE_LOCATION    string = "/tmp/gravwell.comms"
	SMALL_WRITES     int    = 10
	THROTTLE_WRITES  int    = 100
	BENCHMARK_WRITES int    = 250000
	ENTRY_PAD_SIZE   int    = 1024 * 4
	ENTRY_MIN_SIZE   int    = 64
	ENTRY_MAX_SIZE   int    = 512
)

var (
	entryPad []byte
	entIp    net.IP
)

func init() {
	err := cleanup()
	if err != nil {
		fmt.Printf("Failed to clean up pre-run: \"%s\"\n", err.Error())
		os.Exit(-1)
	}
	entryPad = make([]byte, ENTRY_PAD_SIZE)
	for i := 0; i < ENTRY_PAD_SIZE; i++ {
		entryPad[i] = byte(rand.Intn(0xff))
	}
	rand.Seed(0xBEEF3)
	entIp = net.ParseIP("127.0.0.1")

	runtime.GOMAXPROCS(2)
}

func TestInit(t *testing.T) {
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	lst, cli, srv, err := getConnections()
	if err != nil {
		t.Fatal(err)
	}

	etSrv, err := NewEntryReader(srv)
	if err != nil {
		t.Fatal(err)
	}
	etSrv.Start()

	etCli, err := NewEntryWriter(cli)
	if err != nil {
		t.Fatal(err)
	}

	if err := etCli.Close(); err != nil {
		t.Fatal(err)
	}
	if err := etSrv.Close(); err != nil {
		t.Fatal(err)
	}
	if err := closeConnections(cli, srv); err != nil {
		t.Fatal(err)
	}
	lst.Close()
}

func TestSingleRead(t *testing.T) {
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	performCycles(t, 1)
}

func TestSmallRead(t *testing.T) {
	err := cleanup()
	if err != nil {
		t.Fatal(err)
	}
	performCycles(t, SMALL_WRITES)
}

func TestSmallBatch(t *testing.T) {
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	performBatchCycles(t, SMALL_WRITES)
}

func TestThrottled(t *testing.T) {
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	performThrottleCycles(t, THROTTLE_WRITES)
}

func TestWriterOutstandingMismatch(t *testing.T) {
	wtrCfg := EntryReaderWriterConfig{
		OutstandingEntryCount: 2,
		BufferSize:            64 * 1024,
		//Timeout:               time.Second * 2,
		Timeout: 50 * time.Millisecond,
	}
	rdrCfg := EntryReaderWriterConfig{
		OutstandingEntryCount: 256,
		BufferSize:            16 * 1024,
		Timeout:               time.Second * 2,
	}
	outstandingMismatchCycle(rdrCfg, wtrCfg, 16, 16, t)
	outstandingMismatchCycle(rdrCfg, wtrCfg, 32, 4, t)
	outstandingMismatchCycle(rdrCfg, wtrCfg, 16, 8, t)
}

func TestReaderOutstandingMismatch(t *testing.T) {
	wtrCfg := EntryReaderWriterConfig{
		OutstandingEntryCount: 256,
		BufferSize:            16 * 1024,
		Timeout:               time.Second * 2,
	}
	rdrCfg := EntryReaderWriterConfig{
		OutstandingEntryCount: 16,
		BufferSize:            64 * 1024,
		Timeout:               time.Second * 2,
	}
	outstandingMismatchCycle(rdrCfg, wtrCfg, 64, 64, t)
	outstandingMismatchCycle(rdrCfg, wtrCfg, 32, 4, t)
	outstandingMismatchCycle(rdrCfg, wtrCfg, 64, 32, t)
}

func outstandingMismatchCycle(rdrCfg, wtrCfg EntryReaderWriterConfig, count, segments int, t *testing.T) {
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	var totalBytes uint64
	var seg int
	errChan := make(chan error)
	lst, cli, srv, err := getConnections()
	if err != nil {
		t.Fatal(err)
	}
	wtrCfg.Conn = cli
	rdrCfg.Conn = srv

	etSrv, err := NewEntryReaderEx(rdrCfg)
	if err != nil {
		t.Fatal(err)
	}
	etSrv.Start()

	etCli, err := NewEntryWriterEx(wtrCfg)
	if err != nil {
		t.Fatal(err)
	}
	go reader(etSrv, count, segments, errChan)
	for i := 0; i < count; i++ {
		ent := makeEntry()
		if ent == nil {
			t.Fatal("got a nil entry")
		}
		totalBytes += ent.Size()
		if err = etCli.Write(ent); err != nil {
			t.Fatal(err)
		}
		seg++
		if seg == segments {
			time.Sleep(90 * time.Millisecond)
			seg = 0
		}
	}
	if err = etCli.ForceAck(); err != nil {
		t.Fatal(err)
	}
	if err = etCli.Ping(); err != nil {
		t.Fatal(err)
	}
	if err = etCli.Close(); err != nil {
		t.Fatal(err)
	}
	//get the response from the reader
	if err = <-errChan; err != nil {
		t.Fatal(err)
	}
	if err = etSrv.Close(); err != nil {
		t.Fatal(err)
	}
	if err = closeConnections(cli, srv); err != nil {
		t.Fatal(err)
	}
	lst.Close()
}

func TestCleanup(t *testing.T) {
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
}

func performThrottleCycles(t *testing.T, count int) (time.Duration, uint64) {
	return performReaderCycles(t, count, 10)
}

func performCycles(t *testing.T, count int) (time.Duration, uint64) {
	return performReaderCycles(t, count, 0xffffffff)
}

func performReaderCycles(t *testing.T, count, segments int) (time.Duration, uint64) {
	var dur time.Duration
	var totalBytes uint64
	var seg int
	errChan := make(chan error)
	lst, cli, srv, err := getConnections()
	if err != nil {
		t.Fatal(err)
	}

	etSrv, err := NewEntryReader(srv)
	if err != nil {
		t.Fatal(err)
	}
	etSrv.Start()

	etCli, err := NewEntryWriter(cli)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	go reader(etSrv, count, segments, errChan)
	for i := 0; i < count; i++ {
		ent := makeEntry()
		if ent == nil {
			t.Fatal("got a nil entry")
		}
		// if an even number > 0 attach up to 128 entries
		if i > 0 && (i%2) == 0 {
			if err = attachEVs(ent, i%128); err != nil {
				t.Fatal("Failed to attache EVs", err)
			}
		}
		totalBytes += ent.Size()
		if err = etCli.Write(ent); err != nil {
			t.Fatal(err)
		}
		seg++
		if seg == segments {
			time.Sleep(90 * time.Millisecond)
			seg = 0
		}
	}
	if err = etCli.ForceAck(); err != nil {
		t.Fatal(err)
	}
	if err = etCli.Ping(); err != nil {
		t.Fatal(err)
	}
	if err = etCli.Close(); err != nil {
		t.Fatal(err)
	}
	//get the response from the reader
	if err = <-errChan; err != nil {
		t.Fatal(err)
	}
	if err = etSrv.Close(); err != nil {
		t.Fatal(err)
	}
	dur = time.Since(start)
	if err = closeConnections(cli, srv); err != nil {
		t.Fatal(err)
	}
	lst.Close()
	return dur, totalBytes
}

func performBatchCycles(t *testing.T, count int) (time.Duration, uint64) {
	var dur time.Duration
	var totalBytes uint64
	var ents [](*entry.Entry)
	var entsIndex int

	errChan := make(chan error)
	lst, cli, srv, err := getConnections()
	if err != nil {
		t.Fatal(err)
	}

	etSrv, err := NewEntryReader(srv)
	if err != nil {
		t.Fatal(err)
	}
	etSrv.Start()

	etCli, err := NewEntryWriter(cli)
	if err != nil {
		t.Fatal(err)
	}
	ents = make([](*entry.Entry), etCli.OptimalBatchWriteSize())
	entsIndex = 0
	go reader(etSrv, count, 0xffffffff, errChan)

	start := time.Now()
	for i := 0; i < count; i++ {
		ent := makeEntry()
		if ent == nil {
			t.Fatal("got a nil entry")
		}
		totalBytes += ent.Size()
		//check if we need to throw a batch
		if entsIndex >= cap(ents) {
			if n, err := etCli.WriteBatch(ents[0:entsIndex]); err != nil {
				t.Fatal(err)
			} else if n != entsIndex {
				t.Fatal("failed to write all entries")
			}
			entsIndex = 0
		}
		ents[entsIndex] = ent
		entsIndex++
	}
	if entsIndex > 0 {
		if n, err := etCli.WriteBatch(ents[0:entsIndex]); err != nil {
			t.Fatal(err)
		} else if n != entsIndex {
			t.Fatal("failed two write full batch")
		}
	}

	if err = etCli.ForceAck(); err != nil {
		t.Fatal(err)
	}
	if err = etCli.Close(); err != nil {
		t.Fatal(err)
	}
	if err = <-errChan; err != nil {
		t.Fatal(err)
	}

	if err = etSrv.Close(); err != nil {
		t.Fatal(err)
	}

	dur = time.Since(start)

	err = closeConnections(cli, srv)
	if err != nil {
		t.Fatal(err)
	}
	lst.Close()
	return dur, totalBytes
}

func BenchmarkSingle(b *testing.B) {
	var totalBytes uint64

	//initialize the system
	b.StopTimer()
	if err := cleanup(); err != nil {
		b.Fatal(err)
	}
	errChan := make(chan error)
	lst, cli, srv, err := getConnections()
	if err != nil {
		b.Fatal(err)
	}

	etSrv, err := NewEntryReader(srv)
	if err != nil {
		b.Fatal(err)
	}
	etSrv.Start()

	etCli, err := NewEntryWriter(cli)
	if err != nil {
		b.Fatal(err)
	}
	go reader(etSrv, b.N, 0xffffffff, errChan)
	b.StartTimer() //done with initialization

	//actually perform benchmark
	for i := 0; i < b.N; i++ {
		ent := makeEntry()
		if ent == nil {
			b.Fatal("got a nil entry")
		}
		totalBytes += ent.Size()
		if err = etCli.Write(ent); err != nil {
			b.Fatal(err)
		}
	}
	if err = etCli.Close(); err != nil {
		b.Fatal(err)
	}
	if err = <-errChan; err != nil {
		b.Fatal(err)
	}
	//We HAVE to close the server side first (reader) or the client will block on close()
	//waiting for confirmations from the reader that are buffered and not flushed yet
	//but if we close the server (reader) first it will force out the confirmations
	//and the client won't block.
	if err = etSrv.Close(); err != nil {
		b.Fatal(err)
	}
	if err = closeConnections(cli, srv); err != nil {
		b.Fatal(err)
	}
	lst.Close()

	b.StopTimer()
	if err = cleanup(); err != nil {
		b.Fatal(err)
	}
	b.StartTimer()
}

func BenchmarkBatch(b *testing.B) {
	var totalBytes uint64

	//initialize the system
	b.StopTimer()
	if err := cleanup(); err != nil {
		b.Fatal(err)
	}
	errChan := make(chan error)
	lst, cli, srv, err := getConnections()
	if err != nil {
		b.Fatal(err)
	}

	etSrv, err := NewEntryReader(srv)
	if err != nil {
		b.Fatal(err)
	}
	etSrv.Start()

	etCli, err := NewEntryWriter(cli)
	if err != nil {
		b.Fatal(err)
	}
	ents := make([](*entry.Entry), etCli.OptimalBatchWriteSize())
	entsIndex := 0
	go reader(etSrv, b.N, 0xffffffff, errChan)
	b.StartTimer()

	//perform actual benchmark
	for i := 0; i < b.N; i++ {
		ent := makeEntry()
		if ent == nil {
			b.Fatal("got a nil entry")
		}
		totalBytes += ent.Size()
		//check if we need to throw a batch
		if entsIndex >= cap(ents) {
			if _, err = etCli.WriteBatch(ents[0:entsIndex]); err != nil {
				b.Fatal(err)
			}
			entsIndex = 0
		}
		ents[entsIndex] = ent
		entsIndex++
	}
	if entsIndex > 0 {
		if _, err = etCli.WriteBatch(ents[0:entsIndex]); err != nil {
			b.Fatal(err)
		}
	}
	if err := etCli.Close(); err != nil {
		b.Fatal(err)
	}
	if err = <-errChan; err != nil {
		b.Fatal(err)
	}

	//We HAVE to close the server side first (reader) or the client will block on close()
	//waiting for confirmations from the reader that are buffered and not flushed yet
	//but if we close the server (reader) first it will force out the confirmations
	//and the client won't block.
	if err := etSrv.Close(); err != nil {
		b.Fatal(err)
	}
	if err := closeConnections(cli, srv); err != nil {
		b.Fatal(err)
	}
	lst.Close()

	b.StopTimer()
	if err := cleanup(); err != nil {
		b.Fatal(err)
	}
	b.StartTimer()
}

func cleanup() error {
	err := os.Remove(PIPE_LOCATION)
	if err != nil && !strings.Contains(err.Error(), "no such file") {
		return err
	}
	return nil
}

func tailFuckery(c chan net.Conn) {
	if rec := recover(); rec != nil {
		c <- nil
	}
}

func getCliConn(outChan chan net.Conn) {
	defer tailFuckery(outChan)
	conn, err := net.Dial("unix", PIPE_LOCATION)
	if err != nil {
		outChan <- nil
	}
	outChan <- conn
}

func getSrvConn(lst *net.Listener, outChan chan net.Conn) {
	defer tailFuckery(outChan)
	if lst == nil {
		outChan <- nil
		return
	}
	conn, err := (*lst).Accept()
	if err != nil {
		outChan <- nil
		return
	}
	outChan <- conn
}

func getConnections() (net.Listener, net.Conn, net.Conn, error) {
	srvChan := make(chan net.Conn)
	cliChan := make(chan net.Conn)
	lst, err := net.Listen("unix", PIPE_LOCATION)
	if err != nil {
		return nil, nil, nil, err
	}
	go getSrvConn(&lst, srvChan)
	time.Sleep(10 * time.Millisecond)

	go getCliConn(cliChan)
	cli := <-cliChan
	srv := <-srvChan
	if cli == nil || srv == nil {
		if cli == nil {
			cli.Close()
		}
		if srv == nil {
			srv.Close()
		}
		return nil, nil, nil, errors.New("Connections failed")
	}
	return lst, cli, srv, nil
}

func closeConnections(cli, srv net.Conn) error {
	if cli != nil {
		cli.Close()
	}
	if srv != nil {
		srv.Close()
	}
	return nil
}

func reader(et *EntryReader, count, seg int, errChan chan error) {
	var cntRead int
	var segRead int
feederLoop:
	for {
		ent, err := et.Read()
		if err != nil {
			if err == io.EOF {
				break feederLoop
			}
			errChan <- err
			return
		}
		segRead++
		if segRead == seg {
			if err = et.SendThrottle(100 * time.Millisecond); err != nil {
				errChan <- err
				return
			}
			segRead = 0
		}
		if ent == nil {
			errChan <- errors.New("Got nil entry")
			return
		}
		cntRead++
	}
	if cntRead != count {
		errChan <- fmt.Errorf("read count invalid: %d != %d", cntRead, count)
	} else {
		errChan <- nil
	}
}

func makeEntry() *entry.Entry {
	sz := rand.Intn(ENTRY_MAX_SIZE)
	start := rand.Intn(ENTRY_PAD_SIZE - sz)
	dt := entryPad[start:(start + sz)]
	return &entry.Entry{
		TS:   entry.Now(),
		SRC:  entIp,
		Tag:  0,
		Data: dt,
	}
}

func makeEntryWithKey(key int64) *entry.Entry {
	sz := rand.Intn(ENTRY_MAX_SIZE)
	start := rand.Intn(ENTRY_PAD_SIZE - sz)
	dt := entryPad[start:(start + sz)]
	var ts entry.Timestamp
	ts.Sec = key
	return &entry.Entry{
		TS:   ts,
		SRC:  entIp,
		Tag:  0,
		Data: dt,
	}
}

func attachEVs(ent *entry.Entry, cnt int) (err error) {
	for i := 0; i < cnt; i++ {
		var val interface{}
		id := uint8(i % 17)
		switch id {
		case 0:
			val = bool(true)
		case 1:
			val = byte(42)
		case 2:
			val = int8(32)
		case 3:
			val = int16(100)
		case 4:
			val = uint16(200)
		case 5:
			val = int32(300)
		case 6:
			val = uint32(400)
		case 7:
			val = int64(10000)
		case 8:
			val = uint64(10000)
		case 9:
			val = float32(9999)
		case 10:
			val = float64(3.14159)
		case 11:
			val = `stuff`
		case 12:
			val = []byte("things and that\r\n\t\x00")
		case 13:
			val = net.ParseIP("192.168.1.1")
		case 14:
			val, _ = net.ParseMAC("22:8c:9a:6b:39:04")
		case 15:
			val = time.Date(2022, 12, 25, 13, 21, 32, 12345, time.UTC)
		case 16:
			val = (time.Minute + 32*time.Second)
		default:
			val = []byte("so... damn... missed it uhuh")
		}
		if err = ent.AddEnumeratedValueEx(fmt.Sprintf("ev %d", i), val); err != nil {
			return
		}
	}
	return
}
