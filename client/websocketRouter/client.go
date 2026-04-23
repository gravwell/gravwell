/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package websocketRouter provides a routing system for the Gravwell websocket protocols
// it is designed to route messages to the appropriate "connection" over a single websocket.
package websocketRouter

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gravwell/gravwell/v4/client/objlog"

	"github.com/gorilla/websocket"
)

const ()

// SubProtoClient is the main client object used to connect to a SubProtoServer and negotiate subproto connections.
type SubProtoClient struct {
	conn               *websocket.Conn
	mtx                sync.Mutex
	subs               map[string]*SubProtoConn
	defaultHandlerChan chan UnkProtoMsg
	active             bool
	objLog             objlog.ObjLog
}

// NewConnection will attempt to connect to a webserver endpoint and upgrade the connection to a websocket.
// Once a websocket connection has been established, it is up to the caller to negotiate subprotocol connections.
// The default subproto connection is established.
func NewConnection(uri string, headers map[string]string, readBufferSize, writeBufferSize int, enforceCert bool) (c *websocket.Conn, err error) {
	//extract destination from the uri
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	var tlsConfig *tls.Config
	if u.Scheme != `ws` {
		tlsConfig = &tls.Config{}
		tlsConfig.InsecureSkipVerify = !enforceCert
	}

	dialer := websocket.Dialer{
		ReadBufferSize:  readBufferSize,
		WriteBufferSize: writeBufferSize,
		TLSClientConfig: tlsConfig,
	}
	//build up our header
	hdr := http.Header{}
	//add in the origin
	hdr.Add("Origin", fmt.Sprintf("%s://%s", u.Scheme, u.Host))
	//add any header values that we need to
	for key, val := range headers {
		hdr.Add(key, val)
	}

	//we ignore the response because we either get through or we do not, no redirects
	var resp *http.Response
	if c, resp, err = dialer.Dial(uri, hdr); err != nil {
		if c != nil {
			c.Close() //just in case something dumb happened
		}
		if resp != nil && resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("bad StatusCode %d", resp.StatusCode)
		} else {
			return nil, err
		}
	}
	return
}

// NewSubProtoClient connects to a remote websocket and negotiates a routingWebsocket.
// A list of required websockets must be provided at the start.
func NewSubProtoClient(uri string, headers map[string]string, readBufferSize, writeBufferSize int, enforceCert bool, subs []string, ol objlog.ObjLog) (*SubProtoClient, error) {
	wsconn, err := NewConnection(uri, headers, readBufferSize, writeBufferSize, enforceCert)
	if err != nil {
		return nil, err
	}

	//we have a good websocket connection, go through the negotiation portion
	subReq := SubProtocolsReq{subs}
	if err = writeDeadLine(wsconn, subProtoNegotiationDeadline, subReq); err != nil {
		wsconn.Close()
		return nil, err
	}
	resp := SubProtocolsResp{}
	if err = readDeadLine(wsconn, subProtoNegotiationDeadline, &resp); err != nil {
		wsconn.Close()
		return nil, err
	}
	if resp != ACK_RESP {
		return nil, errors.New(resp.Resp)
	}

	protoMap := make(map[string]*SubProtoConn, defaultSubProtoCount)

	spc := &SubProtoClient{
		conn:   wsconn,
		mtx:    sync.Mutex{},
		subs:   protoMap,
		active: false,
		objLog: ol,
	}
	for i := range subs {
		err = spc.AddSubProtocol(subs[i])
		if err != nil {
			spc.Close()
			return nil, err
		}
	}
	return spc, nil
}

// Close will terminate the subprotocol client and close all active subprotocol connections.
// Callers with handles on subprotocol connections will receive errors on read or write.
// Closing the parent connection will close the underlying socket, there is no guaruntee
// that any messages read or written asynchronously will be delivered intact.
func (spc *SubProtoClient) Close() error {
	spc.mtx.Lock()
	defer spc.mtx.Unlock()
	if !spc.active {
		return errors.New("Already closed")
	}
	err := spc.conn.Close()
	spc.active = false
	return err
}

func (spc *SubProtoClient) closeSubProtoConns() {
	for _, v := range spc.subs {
		v.Close()
	}
}

// AddSubProtocol adds another subprotocol to the client
// additional protocols can be added at any time so long
// as the client is active.  Adding a subprotocol does not inform
// the other side of the fact
func (spc *SubProtoClient) AddSubProtocol(sub string) error {
	spc.mtx.Lock()
	defer spc.mtx.Unlock()
	if spc.conn == nil {
		return errors.New("Connection closed")
	}

	//check that the subprotocol doesn't already exist
	_, ok := spc.subs[sub]
	if ok {
		return errors.New("Subprotocol already resident")
	}
	subProtChan := make(chan json.RawMessage, defaultMsgDepth)
	subProt := &SubProtoConn{
		subproto: sub,
		ch:       subProtChan,
		sr:       spc,
		active:   1,
		objLog:   spc.objLog,
	}
	spc.subs[sub] = subProt
	return nil
}

// SubProtocols returns the negotiated and/or added subprotocols
func (spc *SubProtoClient) SubProtocols() ([]string, error) {
	spc.mtx.Lock()
	defer spc.mtx.Unlock()
	if spc.conn == nil {
		return nil, errors.New("Connection closed")
	}

	subs := make([]string, 0, len(spc.subs))
	for k := range spc.subs {
		subs = append(subs, k)
	}
	return subs, nil
}

// GetSubProtoConn will attempt to retrieve a previously negotiated subprotocol connection.
// If the subprotocol connection has not been negotiated, an ErrSubProtoNotFound error is returned.
func (spc *SubProtoClient) GetSubProtoConn(subProto string) (*SubProtoConn, error) {
	spc.mtx.Lock()
	defer spc.mtx.Unlock()
	//we allow retrieving subprotoconns even when not-active
	//so that children can fire up prior to starting the routine
	//and also so that data resident in subprotoconns can be retrieved
	//even after teh SubProtoClient has closed
	sub, ok := spc.subs[subProto]
	if !ok {
		return nil, ErrSubProtoNotFound
	}
	return sub, nil
}

/*
CloseSubProtoConn will close a subprotocol connection if it exists

	if the connection does not exist we just return, no error
*/
func (spc *SubProtoClient) CloseSubProtoConn(subProto string) error {
	spc.mtx.Lock()
	defer spc.mtx.Unlock()
	sub, ok := spc.subs[subProto]
	if !ok {
		return nil
	}
	if err := sub.Close(); err != nil {
		return err
	}
	delete(spc.subs, subProto)
	return nil
}

// WriteErrorMsg sends an error down the default subproto connection.
func (spc *SubProtoClient) WriteErrorMsg(err error) error {
	return spc.writeProtoJSON("error", err)
}

// writeProtoJSON allows subProtoConns to call into the parent and actually write.
func (spc *SubProtoClient) writeProtoJSON(proto string, obj interface{}) error {
	spc.mtx.Lock()
	defer spc.mtx.Unlock()
	if spc.conn == nil {
		return errors.New("Connection closed")
	}
	if !spc.active {
		return ErrServerClosed
	}

	return spc.conn.WriteJSON(subProtoSendMsg{proto, obj})
}

// Run fires up the main muxing routine and starts throwing messages to subprotocol connections.
// It waits for the subproto to complete.
func (spc *SubProtoClient) Run() error {
	spc.mtx.Lock()
	if spc.active {
		spc.mtx.Unlock()
		return ErrAlreadyRunning
	}
	spc.active = true
	spc.mtx.Unlock()

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go spc.routine(wg)
	wg.Wait()
	wg = nil
	return nil
}

// Start fires up the main muxing routine and starts throwing messages to subprotocol conns.
// It waits for the routine to start, but will not block while servicing messages.
func (spc *SubProtoClient) Start() error {
	spc.mtx.Lock()
	defer spc.mtx.Unlock()
	if spc.active {
		return ErrAlreadyRunning
	}
	spc.active = true

	go spc.routine(nil)
	return nil
}

// routine is the go routine that acts as the muxer for the various subprotocols
func (spc *SubProtoClient) routine(wg *sync.WaitGroup) {
	var err error
	var errCount int
	//this routine is responsible for closing the subprotoconns
	defer spc.closeSubProtoConns()
	if wg != nil {
		defer wg.Done()
	}
	if spc.conn == nil {
		//if the connection is nil, just leave
		return
	}

	//the routine will stay on the loop indefinitely
	//the "break out" mechanism is a read error on the websocket
	//connection indicating the connection closed
loopExit:
	for spc.active {
		//we have to allocate a new one on every pass due
		//to golang liking to just not write non-existent values
		//so a partial message or nil value would leave the previous
		//value intact, making it appear as if it came too
		spm := subProtoRecvMsg{}

		//no locking is needed here because we only read
		//and are the ONLY thread to do any reading
		if err = spc.conn.ReadJSON(&spm); err != nil {
			if err == io.EOF || strings.Contains(err.Error(), "EOF") {
				//connection closed, we are out
				spc.active = false
				break loopExit
			}
			errCount++
			//check if we are over our error threshhold
			if errCount >= errThreshHold {
				//we are leaving
				spc.active = false
				break loopExit
			}
			continue
		}
		//always reset the error count on a good read
		spc.mtx.Lock()
		//maps are NOT threadsafe
		s, ok := spc.subs[spm.Type]
		spc.mtx.Unlock()
		if !ok {
			//check if we can write to the default
			if spc.defaultHandlerChan != nil {
				select {
				case spc.defaultHandlerChan <- UnkProtoMsg(spm):
				default:
					s = nil
				}
			} else {
				//we ditch unknown subprotocols if no default
				s = nil
			}
		} else {
			//a failure to hand a message to a subconn is global failure
			if err = s.AddMessage(spm.Data); err != nil {
				errCount++
				continue
			}
		}
		errCount = 0
	}
}
