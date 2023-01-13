/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package websocketRouter

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gravwell/gravwell/v3/client/objlog"

	"github.com/gorilla/websocket"
)

const (
	SecWebsocketProtocol string = `Sec-Websocket-Protocol`
	serverReadTimeout           = 90 * time.Second
)

var (
	nilTime             = time.Time{}
	ErrZeroSubProtocols = errors.New("requested zero subprotocols")
)

// SubProtoServer is the main routing system for use in handling new clients.
// A server is in charge of upgrading a websocket to a subprotocol websocket and routing messages to the appropriate subprotocon.
type SubProtoServer struct {
	conn               *websocket.Conn
	wg                 sync.WaitGroup
	mtx                sync.Mutex
	subs               map[string]*SubProtoConn
	defaultHandlerChan chan UnkProtoMsg
	active             bool
	objLog             objlog.ObjLog
}

// checkOrigin returns true if the origin is not set or is equal to the request host.
func checkOrigin(r *http.Request, allowedOrigin string) bool {
	if allowedOrigin == "*" {
		return true
	}

	origin := r.Header["Origin"]
	if len(origin) == 0 {
		return true
	}
	u, err := url.Parse(origin[0])
	if err != nil {
		return false
	}
	ao, err := url.Parse(allowedOrigin)
	if err != nil {
		return false
	}

	if equalASCIIFold(u.Host, r.Host) || equalASCIIFold(u.Host, ao.Host) {
		return true
	}

	return false
}

// equalASCIIFold returns true if s is equal to t with ASCII case folding as
// defined in RFC 4790.
func equalASCIIFold(s, t string) bool {
	for s != "" && t != "" {
		sr, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		tr, size := utf8.DecodeRuneInString(t)
		t = t[size:]
		if sr == tr {
			continue
		}
		if 'A' <= sr && sr <= 'Z' {
			sr = sr + 'a' - 'A'
		}
		if 'A' <= tr && tr <= 'Z' {
			tr = tr + 'a' - 'A'
		}
		if sr != tr {
			return false
		}
	}
	return s == t
}

// NewSubProtoServer upgrades a connection to a websocket and attempts to instantiate subprotocol
// routers.  If no subprotocols are specified in the handshake NewSubProtoServer returns an error
func NewSubProtoServer(w http.ResponseWriter, r *http.Request, readBufferSize, writeBufferSize int, allowedOrigin string) (*SubProtoServer, error) {
	//ensure the buffers provided are sane
	if readBufferSize <= 0 {
		readBufferSize = defaultBufferSize
	}
	if writeBufferSize <= 0 {
		writeBufferSize = defaultBufferSize
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  readBufferSize,
		WriteBufferSize: writeBufferSize,
		//we just throw the negotiated subprocol as ACK
		//this is out of band anyway
		CheckOrigin: func(r *http.Request) bool {
			return checkOrigin(r, allowedOrigin)
		},
	}

	//add in whatever requested subprotocols they said
	//because we don't care, and the websocket API is
	//incredibly dumb.  Whoever thought this out sucks at life
	subs, ok := r.Header[SecWebsocketProtocol]
	if ok {
		upgrader.Subprotocols = subs
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return nil, err
	}

	//attempt to get the subprotocols requested
	reqSubs := SubProtocolsReq{}
	err = readDeadLine(conn, subProtoNegotiationDeadline, &reqSubs)
	if err != nil {
		conn.Close()
		return nil, err
	}
	subprotos := reqSubs.Subs
	if len(subprotos) <= 0 {
		writeDeadLine(conn, subProtoNegotiationDeadline, ERR_RESP)
		conn.Close()
		return nil, ErrZeroSubProtocols
	}

	if err = writeDeadLine(conn, subProtoNegotiationDeadline, ACK_RESP); err != nil {
		conn.Close()
		return nil, err
	}
	ol, err := objlog.NewNilLogger()
	if err != nil {
		return nil, err
	}

	//build out the actual subproto server
	protoMap := make(map[string]*SubProtoConn, defaultSubProtoCount)
	SPS := &SubProtoServer{
		conn:   conn,
		mtx:    sync.Mutex{},
		subs:   protoMap,
		objLog: ol,
	}

	//for each subprotocol initiate a subprotocol handler
	for i := range subprotos {
		if err = SPS.AddSubProtocol(subprotos[i]); err != nil {
			SPS.Close()
			SPS = nil
			return nil, err
		}
	}

	//all the negotiated subprotocols are up and ready
	//send our new baby out into the world
	return SPS, nil
}

// GetDefaultMessageChan gets direct access to the channel receiving default messages
func (ss *SubProtoServer) GetDefaultMessageChan() (chan UnkProtoMsg, error) {
	if ss.defaultHandlerChan == nil {
		ss.mtx.Lock()
		ss.defaultHandlerChan = make(chan UnkProtoMsg, defaultMsgDepth)
		ss.mtx.Unlock()
	}
	return ss.defaultHandlerChan, nil
}

// WriteDefaultMessage sends an object down the default subproto connection.
func (ss *SubProtoServer) WriteDefaultMessage(proto string, obj interface{}) error {
	return ss.writeProtoJSON(proto, obj)
}

// Close closes the websocket connection and closes all the subproto connections
// subprotoconns can still retrieve data after a close
func (ss *SubProtoServer) Close() error {
	ss.mtx.Lock()
	if !ss.active {
		ss.mtx.Unlock()
		return ErrServerClosed
	}
	//close all the conns
	for _, v := range ss.subs {
		v.Close()
	}
	//close the master web socket, this should close the routine if active
	ss.conn.Close()
	ss.active = false

	//make sure we release the mutex while we wait, the routine
	//expects to be able to grab it occaisionally
	ss.mtx.Unlock()

	//Wait for everyone to exit
	ss.wg.Wait()

	//relock the mutex and continue on
	ss.mtx.Lock()
	defer ss.mtx.Unlock()

	//close the default channel if it exists
	if ss.defaultHandlerChan != nil {
		close(ss.defaultHandlerChan)
		ss.defaultHandlerChan = nil
	}
	return nil
}

// close any un-negotiated default subproto connections.
func (ss *SubProtoServer) closeSubProtoConns() {
	ss.mtx.Lock()
	defer ss.mtx.Unlock()
	for _, v := range ss.subs {
		v.Close()
	}
}

// Run fires up the main muxing routine and starts throwing messages to sub.
// This routine blocks.
func (ss *SubProtoServer) Run() error {
	ss.mtx.Lock()
	if ss.active {
		ss.mtx.Unlock()
		return ErrAlreadyRunning
	}
	ss.active = true
	ss.mtx.Unlock()

	ss.wg.Add(1) //the routine is synchronous here, but it issues a done, so we have to add
	ss.routine(nil)
	return nil
}

// Start fires up the main muxing routine and starts throwing messages to sub.
// This routine DOES NOT block.
func (ss *SubProtoServer) Start() error {
	ss.mtx.Lock()
	if ss.active {
		ss.mtx.Unlock()
		return ErrAlreadyRunning
	}
	ss.active = true
	ss.mtx.Unlock()

	errCh := make(chan error, 1)
	ss.wg.Add(1)
	go ss.routine(errCh)
	return <-errCh
}

// AddSubProtocol adds another subprotocol to our router
// these can be added at any time so long as the connection is active
func (ss *SubProtoServer) AddSubProtocol(subproto string) error {
	//sanity check for stupidity
	if ss == nil {
		return errors.New("No parent router")
	}

	ss.mtx.Lock()
	defer ss.mtx.Unlock()
	//the connection must ALWAYS be checked AFTER holding the mutex
	//as anyone can close it
	if ss.conn == nil {
		return errors.New("Connection closed")
	}

	//check that the subprotocol doesn't already exist
	_, ok := ss.subs[subproto]
	if ok {
		return errors.New("Subprotocol already resident")
	}

	//its an original
	spcChan := make(chan json.RawMessage, defaultMsgDepth)
	spc := &SubProtoConn{
		subproto: subproto,
		ch:       spcChan,
		sr:       ss,
		active:   1,
		objLog:   ss.objLog,
	}
	ss.subs[subproto] = spc //lock already held
	return nil
}

// SubProtocols returns the negotiated and/or added subprotocols
func (ss *SubProtoServer) SubProtocols() ([]string, error) {
	ss.mtx.Lock()
	defer ss.mtx.Unlock()
	if ss.conn == nil {
		return nil, errors.New("Connection closed")
	}

	subs := make([]string, 0, len(ss.subs))
	for k, _ := range ss.subs {
		subs = append(subs, k)
	}
	return subs, nil
}

// GetSubProtoconn gets a subprotocol talker using the named subProto.
// If the named subProto is not found, ErrSubProtoNotFound is returned
func (ss *SubProtoServer) GetSubProtoConn(subProto string) (*SubProtoConn, error) {
	ss.mtx.Lock()
	defer ss.mtx.Unlock()
	//we allow retrieving subprotoconns even when not-active
	//so that children can fire up prior to starting the routine
	//and also so that data resident in subprotoconns can be retrieved
	//even after teh SubProtoServer has closed
	spc, ok := ss.subs[subProto]
	if !ok {
		return nil, ErrSubProtoNotFound
	}
	return spc, nil
}

// WriteErrorMsg sends an error message down the "erro" protocol channel
func (ss *SubProtoServer) WriteErrorMsg(err error) error {
	return ss.writeProtoJSON("error", err)
}

// writeProtoJSON allows subProtoConns to call into the parent and actually write
func (ss *SubProtoServer) writeProtoJSON(proto string, obj interface{}) error {
	ss.mtx.Lock()
	defer ss.mtx.Unlock()
	if ss.conn == nil {
		return errors.New("Connection closed")
	}
	if !ss.active {
		return ErrServerClosed
	}

	err := ss.conn.WriteJSON(subProtoSendMsg{proto, obj})
	if err == nil {
		// On successful write, reset the *read* deadline because we had successful network traffic
		// May be a bit weird but it helps in situations like the stats websocket.
		if err := ss.conn.SetReadDeadline(time.Now().Add(serverReadTimeout)); err != nil {
			return err
		}
	}
	return err
}

// routine is the routine that acts as the muxer for the various subprotocols
func (ss *SubProtoServer) routine(errCh chan error) {
	defer ss.wg.Done()
	var errCount int
	//this routine is responsible for closing the subprotoconns
	defer ss.closeSubProtoConns()
	if ss.conn == nil {
		if errCh != nil {
			errCh <- errors.New("connection is nil")
			close(errCh)
		}
		//if the connection is nil, just leave
		ss.active = false
		return
	}

	if errCh != nil {
		errCh <- nil
		close(errCh)
	}
	//the routine will stay on the loop indefinitely
	//the "break out" mechanism is a read error on the websocket
	//connection indicating the connection closed
loopExit:
	for ss.active {
		//we have to allocate a new one on every pass due
		//to golang liking to just not write non-existent values
		//so a partial message or nil value would leave the previous
		//value intact, making it appear as if it came too
		spm := subProtoRecvMsg{}

		//no locking is needed here because we only read
		//and are the ONLY thread to do any reading
		if err := ss.conn.SetReadDeadline(time.Now().Add(serverReadTimeout)); err != nil {
			break loopExit
		}
		if err := ss.conn.ReadJSON(&spm); err != nil {
			if err == io.EOF {
				//connection closed, we are out
				break loopExit
			} else if websocket.IsCloseError(err, 1005) {
				break loopExit
			}
			errCount++
			//check if we are over our error threshhold
			if errCount >= errThreshHold {
				//we are leaving
				//NOW we have to grab the mutex
				break loopExit
			}
			continue
		}
		if err := ss.conn.SetReadDeadline(nilTime); err != nil {
			break loopExit
		}
		//always reset the error count on a good read
		ss.mtx.Lock()
		//maps are NOT threadsafe
		spc, ok := ss.subs[spm.Type]
		ss.mtx.Unlock()
		if !ok {
			//check if we can write to the default
			if ss.defaultHandlerChan != nil {
				select {
				case ss.defaultHandlerChan <- UnkProtoMsg{spm.Type, spm.Data}:
				default:
					spc = nil
				}
			} else {
				//we ditch unknown subprotocols if no default
				spc = nil
			}
		} else {
			//a failure to hand a message to a subconn is global failure
			if err := spc.AddMessage(spm.Data); err != nil {
				errCount++
				continue
			}
		}
		errCount = 0
	}
	ss.active = false
	ss.conn.Close()
}
