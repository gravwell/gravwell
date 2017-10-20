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
	"math/rand"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/ingest/entry"
)

const (
	PIPE_LOCATION    string = "/tmp/gravwell.comms"
	SMALL_WRITES     int    = 10
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
	err := cleanup()
	if err != nil {
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
	err := cleanup()
	if err != nil {
		t.Fatal(err)
	}
	performBatchCycles(t, SMALL_WRITES)
}

func TestCleanup(t *testing.T) {
	err := cleanup()
	if err != nil {
		t.Fatal(err)
	}
}

func performCycles(t *testing.T, count int) (time.Duration, uint64) {
	var dur time.Duration
	var totalBytes uint64
	errChan := make(chan error)
	lst, cli, srv, err := getConnections()
	if err != nil {
		t.Fatal(err)
	}

	etSrv, err := NewEntryReader(srv)
	if err != nil {
		t.Fatal(err)
	}

	etCli, err := NewEntryWriter(cli)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	go reader(etSrv, count, errChan)
	for i := 0; i < count; i++ {
		ent := makeEntry()
		if ent == nil {
			t.Fatal("got a nil entry")
		}
		totalBytes += ent.Size()
		err = etCli.Write(ent)
		if err != nil {
			t.Fatal(err)
		}
	}
	err = <-errChan
	if err != nil {
		t.Fatal(err)
	}
	//We HAVE to close the server side first (reader) or the client will block on close()
	//waiting for confirmations from the reader that are buffered and not flushed yet
	//but if we close the server (reader) first it will force out the confirmations and
	//and the client won't block.
	err = etSrv.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = etCli.Close()
	if err != nil {
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

	etCli, err := NewEntryWriter(cli)
	if err != nil {
		t.Fatal(err)
	}
	ents = make([](*entry.Entry), etCli.OptimalBatchWriteSize())
	entsIndex = 0
	go reader(etSrv, count, errChan)

	start := time.Now()
	for i := 0; i < count; i++ {
		ent := makeEntry()
		if ent == nil {
			t.Fatal("got a nil entry")
		}
		totalBytes += ent.Size()
		//check if we need to throw a batch
		if entsIndex >= cap(ents) {
			err = etCli.WriteBatch(ents[0:entsIndex])
			if err != nil {
				t.Fatal(err)
			}
			entsIndex = 0
		}
		ents[entsIndex] = ent
		entsIndex++
	}
	if entsIndex > 0 {
		err = etCli.WriteBatch(ents[0:entsIndex])
		if err != nil {
			t.Fatal(err)
		}
	}
	err = <-errChan
	if err != nil {
		t.Fatal(err)
	}

	//We HAVE to close the server side first (reader) or the client will block on close()
	//waiting for confirmations from the reader that are buffered and not flushed yet
	//but if we close the server (reader) first it will force out the confirmations and
	//and the client won't block.
	err = etSrv.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = etCli.Close()
	if err != nil {
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

	//intialize the system
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

	etCli, err := NewEntryWriter(cli)
	if err != nil {
		b.Fatal(err)
	}
	go reader(etSrv, b.N, errChan)
	b.StartTimer() //done with initialization

	//actually perform benchmark
	for i := 0; i < b.N; i++ {
		ent := makeEntry()
		if ent == nil {
			b.Fatal("got a nil entry")
		}
		totalBytes += ent.Size()
		err = etCli.Write(ent)
		if err != nil {
			b.Fatal(err)
		}
	}
	if err := <-errChan; err != nil {
		b.Fatal(err)
	}
	//We HAVE to close the server side first (reader) or the client will block on close()
	//waiting for confirmations from the reader that are buffered and not flushed yet
	//but if we close the server (reader) first it will force out the confirmations and
	//and the client won't block.
	if err := etSrv.Close(); err != nil {
		b.Fatal(err)
	}
	if err := etCli.Close(); err != nil {
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

func BenchmarkBatch(b *testing.B) {
	var totalBytes uint64

	//intialize the system
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

	etCli, err := NewEntryWriter(cli)
	if err != nil {
		b.Fatal(err)
	}
	ents := make([](*entry.Entry), etCli.OptimalBatchWriteSize())
	entsIndex := 0
	go reader(etSrv, b.N, errChan)
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
			err = etCli.WriteBatch(ents[0:entsIndex])
			if err != nil {
				b.Fatal(err)
			}
			entsIndex = 0
		}
		ents[entsIndex] = ent
		entsIndex++
	}
	if entsIndex > 0 {
		err = etCli.WriteBatch(ents[0:entsIndex])
		if err != nil {
			b.Fatal(err)
		}
	}
	err = <-errChan
	if err != nil {
		b.Fatal(err)
	}

	//We HAVE to close the server side first (reader) or the client will block on close()
	//waiting for confirmations from the reader that are buffered and not flushed yet
	//but if we close the server (reader) first it will force out the confirmations and
	//and the client won't block.
	if err := etSrv.Close(); err != nil {
		b.Fatal(err)
	}
	if err := etCli.Close(); err != nil {
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

func reader(et *EntryReader, count int, errChan chan error) {
	for i := 0; i < count; i++ {
		ent, err := et.Read()
		if err != nil {
			errChan <- err
			return
		}
		if ent == nil {
			errChan <- errors.New("Got nil entry")
			return
		}
	}
	errChan <- nil
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
