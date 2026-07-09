/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package websocketRouter

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/client/objlog"
)

const (
	buffSize          int = 1024
	testTimeout           = time.Second * 10
	talkerMsgCount        = 128
	hammerTalkerCount     = 2048
)

var (
	defaultSubs = []string{"testA", "testB", "testC", "ChuckTesta"}
	lg          objlog.ObjLog
	headers     = map[string]string{`Sec-Websocket-Protocol`: `LEGITJWT`}
)

type testSet struct {
	lst     net.Listener
	srv     *http.Server
	hndlr   *handler
	dstUri  string
	wsUri   string
	lstPort int
	done    chan error
}

func (ts *testSet) run() error {
	if ts.done == nil {
		return errors.New("nil chan")
	}
	go ts.serve()
	return nil
}

func (ts *testSet) serve() {
	err := ts.srv.Serve(ts.lst)
	if err == http.ErrServerClosed {
		err = nil
	}
	ts.done <- err
}

func (ts *testSet) Close() (err error) {
	if ts.lst == nil || ts.hndlr == nil || ts.srv == nil {
		err = errors.New("already closed")
		return
	}
	if err = ts.srv.Close(); err != nil {
		return
	}
	if err = <-ts.done; err != nil {
		return
	}
	ts.lst.Close() //just in case
	if err = ts.hndlr.Close(); err != nil {
		return
	}
	close(ts.done)
	return
}

func TestMain(m *testing.M) {
	l, err := objlog.NewNilLogger()
	if err != nil {
		fmt.Println("Failed to startup object logger")
		os.Exit(-1)
	}
	lg = l
	if r := m.Run(); r != 0 {
		os.Exit(r)
	}
}

// fire up the server and stop it
func TestStartStop(t *testing.T) {
	ts, err := startWebserver()
	if err != nil {
		t.Fatal(err)
	} else if err = ts.hndlr.Error(); err != nil {
		t.Fatal(err)
	} else if err = ts.Close(); err != nil {
		t.Fatal(err)
	}
}

// hit the server with a non-websocket client, it should puke
func TestBadClient(t *testing.T) {
	ts, err := startWebserver()
	if err != nil {
		t.Fatal(err)
	} else if err = ts.hndlr.Error(); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{
		Timeout: time.Second,
	}
	resp, err := client.Get(ts.dstUri)
	if err != nil {
		ts.Close()
		t.Fatal(err)
	}
	if err = ts.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("A bad client failed to get BadRequestCode: %s", resp.Status)
	}
}

// hit the server with a client an immediately disconnect
// just perform the handshake
func TestGoodClient(t *testing.T) {
	ts, err := startWebserver()
	if err != nil {
		t.Fatal(err)
	} else if err = ts.hndlr.Error(); err != nil {
		t.Fatal(err)
	}

	spc, err := NewSubProtoClient(ts.wsUri, headers, buffSize, buffSize, false, defaultSubs, lg)
	if err != nil {
		t.Fatal(err)
	}

	spc.Close()
	if err = ts.Close(); err != nil {
		t.Fatal(err)
	}
}

// hit the server with a client and fire up talkers to actually route
// messages.
func TestGoodClientTalking(t *testing.T) {
	subIDs := defaultSubs
	testGoodClientTalking(t, subIDs, testTimeout)
}

// test an authenticated client
func TestAuthedGoodClientTalking(t *testing.T) {
	var subIDs []string
	for i := 0; i < 4; i++ {
		subIDs = append(subIDs, fmt.Sprintf("sub_%d", i))
	}
	testGoodClientTalking(t, subIDs, testTimeout)
}

// test an authenticated client
func TestBadAuthGoodClientTalking(t *testing.T) {
	ts, err := startWebserver()
	if err != nil {
		t.Fatal(err)
	} else if err = ts.hndlr.Error(); err != nil {
		t.Fatal(err)
	}

	badHeaders := map[string]string{`Sec-Websocket-Protocol`: `NOTLEGITJWT`}
	spc, err := NewSubProtoClient(ts.wsUri, badHeaders, buffSize, buffSize, false, defaultSubs, lg)
	if err == nil {
		t.Fatal("Failed to catch the bad status code")
	} else if spc != nil {
		t.Fatal("Got a client back on an error state")
	}

	if err = ts.Close(); err != nil {
		t.Fatal(err)
	}
}

// hammer the system to see how it holds up
// this will also fork off many go routines...
func TestHammerGoodClientTalking(t *testing.T) {
	var subIDs []string
	runtime.GOMAXPROCS(runtime.NumCPU()) //give it some threads on this one
	for i := 0; i < hammerTalkerCount; i++ {
		subIDs = append(subIDs, fmt.Sprintf("sub_%d", i))
	}
	testGoodClientTalking(t, subIDs, 0)
}

func testGoodClientTalking(t *testing.T, subIDs []string, to time.Duration) {
	ts, err := startWebserver()
	if err != nil {
		t.Fatal(err)
	} else if err = ts.hndlr.Error(); err != nil {
		t.Fatal(err)
	}

	spc, err := NewSubProtoClient(ts.wsUri, headers, buffSize, buffSize, false, subIDs, lg)
	if err != nil {
		t.Fatal(err)
	}
	doneChan := make(chan error, len(subIDs))

	go func() {
		if err := spc.Run(); err != nil {
			t.Error(err)
		}
	}()
	//there is a race condition here where we get up and start trying to write before
	//the server can actually start its go routines, this race condition won't ever matter
	//in the real setup, because a client can't connect and start hammering the server
	//before the server comes up, because we start the routines differently.
	//This is purely an artifact of the way we test.  Hints, the sleep
	time.Sleep(10 * time.Millisecond)

	for i := range subIDs {
		c, err := spc.GetSubProtoConn(subIDs[i])
		if err != nil {
			t.Fatal(err)
		}
		go clientTalker(c, doneChan)
	}
	tmr := time.NewTimer(to)
	defer tmr.Stop()
	if to == 0 {
		tmr.Stop()
	}
	for i := 0; i < len(subIDs); i++ {
		select {
		case err := <-doneChan:
			if err != nil {
				t.Fatal(err)
			}
		case <-tmr.C:
			t.Fatal("TIMEOUT")
		}
	}

	spc.Close()
	if err = ts.Close(); err != nil {
		t.Fatal(err)
	}
}

// test the procedure for booting clients
func TestBootClients(t *testing.T) {
	//get the server up
	ts, err := startWebserver()
	if err != nil {
		t.Fatal(err)
	} else if err = ts.hndlr.Error(); err != nil {
		t.Fatal(err)
	}

	var conns []*SubProtoClient

	//create 10 clients and kick them all off
	//wait for each to let us know they hit their read state
	readyChan := make(chan bool, 10)
	defer close(readyChan)
	doneChan := make(chan error, 10)
	defer close(doneChan)
	for i := 0; i < 10; i++ {
		spc, err := NewSubProtoClient(ts.wsUri, headers, buffSize, buffSize, false, defaultSubs, lg)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, spc)
		go bootableClient(spc, readyChan, doneChan)
	}

	tmr := time.NewTimer(500 * time.Millisecond)
	for i := 0; i < 10; i++ {
		select {
		case <-readyChan:
		case <-tmr.C:
			t.Fatal("Timed out waiting for client ok")
		}
	}
	tmr.Stop()

	//boot all the clients
	for _, v := range conns {
		if err := v.Close(); err != nil {
			t.Fatal(err)
		}
	}

	tmr.Reset(500 * time.Millisecond)
	//wait for the clients to exit, with a timeout
	for i := 0; i < 10; i++ {
		select {
		case err := <-doneChan:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("Timed out waiting for clients to return")
		}
	}
	if err = ts.Close(); err != nil {
		t.Fatal(err)
	}
}

func bootableClient(c *SubProtoClient, readyChan chan bool, doneChan chan error) {
	defer c.Close()
	readyChan <- true
	var stuff string
	go func() {
		if err := c.Run(); err != nil {
			doneChan <- err
		}
	}()
	conn, err := c.GetSubProtoConn("testA")
	if err != nil {
		doneChan <- err
		return
	}
	if err := conn.ReadJSON(&stuff); err != nil {
		if err != io.EOF {
			doneChan <- err
			return
		}
	}
	doneChan <- nil
}

// test the procedure for booting clients with just a connection
func TestBootClientConns(t *testing.T) {
	//get the server up
	ts, err := startWebserver()
	if err != nil {
		t.Fatal(err)
	} else if err = ts.hndlr.Error(); err != nil {
		t.Fatal(err)
	}

	var conns []*SubProtoConn

	//create 10 clients and kick them all off
	//wait for each to let us know they hit their read state
	readyChan := make(chan bool, 10)
	defer close(readyChan)
	doneChan := make(chan error, 10)
	defer close(doneChan)
	for i := 0; i < 10; i++ {
		spc, err := NewSubProtoClient(ts.wsUri, headers, buffSize, buffSize, false, defaultSubs, lg)
		if err != nil {
			t.Fatal(err)
		}
		go spc.Run()
		defer spc.Close()
		conn, err := spc.GetSubProtoConn("testA")
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
		go bootableClientConn(conn, readyChan, doneChan)
	}

	tmr := time.NewTimer(500 * time.Millisecond)
	for i := 0; i < 10; i++ {
		select {
		case <-readyChan:
		case <-tmr.C:
			t.Fatal("Timed out waiting for client ok")
		}
	}
	tmr.Stop()

	//boot all the clients
	for _, v := range conns {
		if err := v.Close(); err != nil {
			t.Fatal(err)
		}
	}

	tmr.Reset(500 * time.Millisecond)
	//wait for the clients to exit, with a timeout
	for i := 0; i < 10; i++ {
		select {
		case err := <-doneChan:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("Timed out waiting for clients to return")
		}
	}
	if err = ts.Close(); err != nil {
		t.Fatal(err)
	}
}

type JSONReaderWriter interface {
	ReadJSON(interface{}) error
	WriteJSON(interface{}) error
	Close() error
}

func bootableClientConn(conn JSONReaderWriter, readyChan chan bool, doneChan chan error) {
	defer conn.Close()
	readyChan <- true
	var stuff string
	if err := conn.ReadJSON(&stuff); err != nil {
		if err != io.EOF {
			doneChan <- err
			return
		}
	}
	doneChan <- nil
}

func waitGroupSignal(wg *sync.WaitGroup, ch chan bool) {
	wg.Wait()
	ch <- true
}

func clientTalker(c *SubProtoConn, doneChan chan error) {
	msgID := 1
	defer c.Close()
	for i := 0; i < talkerMsgCount; i++ {
		m := msg{}
		m.Msg = "HEEEEYOOOO!"
		m.ID = msgID
		//send the message
		if err := c.WriteJSON(m); err != nil {
			doneChan <- err
			return
		}
		//receive the response
		if err := c.ReadJSON(&m); err != nil {
			doneChan <- err
			return
		}
		if m.ID != (msgID + 1) {
			doneChan <- fmt.Errorf("Invalid message ID response")
			return
		}
	}
	doneChan <- nil
}

func startWebserver() (ts testSet, err error) {
	if ts.lstPort, err = getFreeTCPPort(); err != nil {
		return
	}
	if ts.lst, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", ts.lstPort)); err != nil {
		return
	}
	ts.hndlr = &handler{}
	ts.srv = &http.Server{
		Handler:      ts.hndlr,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	}
	ts.done = make(chan error, 1)
	ts.dstUri = fmt.Sprintf("http://127.0.0.1:%d/", ts.lstPort)
	ts.wsUri = fmt.Sprintf("ws://127.0.0.1:%d/", ts.lstPort)
	err = ts.run()
	return
}

type handler struct {
	spr *SubProtoServer
	err error
}

type msg struct {
	Msg string
	ID  int
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for k, v := range headers {
		if r.Header.Get(k) != v {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}
	spr, err := NewSubProtoServer(w, r, buffSize, buffSize, "")
	if err != nil {
		h.err = err
		return
	}

	//grab all the subprotocol handlers and bark off handlers for each
	subs, err := spr.SubProtocols()
	if err != nil {
		h.err = err
		spr.Close()
		return
	}
	wg := &sync.WaitGroup{}
	for i := range subs {
		c, err := spr.GetSubProtoConn(subs[i])
		if err != nil {
			h.err = err
			spr.Close()
			return
		}
		wg.Add(1)
		go subProtoClientHandler(c, wg)
	}

	if err = spr.Run(); err != nil {
		h.err = err
	}
	wg.Wait()
	if err = spr.Close(); err != nil {
		h.err = err
	}
}

func subProtoClientHandler(c *SubProtoConn, wg *sync.WaitGroup) {
	for {
		m := msg{}
		//read a message
		err := c.ReadJSON(&m)
		if err != nil {
			break
		}
		//send back "PONG" message with an ID+1
		m.Msg = "PONG"
		m.ID++
		err = c.WriteJSON(m)
		if err != nil {
			break
		}
	}
}

func (h *handler) Close() error {
	if h.spr == nil {
		return nil
	}
	if err := h.spr.Close(); err != nil {
		return err
	}
	return nil
}

func (h *handler) Error() error {
	if h.err != nil {
		err := h.err
		h.err = nil
		return err
	}
	return nil
}

// Get a free TCP port
func getFreeTCPPort() (int, error) {
	a, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return -1, err
	}
	l, err := net.ListenTCP("tcp", a)
	if err != nil {
		return -1, err
	}
	ta, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return -1, errors.New("Failed to resolve port")
	}
	if err := l.Close(); err != nil {
		return -1, err
	}
	return ta.Port, nil
}
