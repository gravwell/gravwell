/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package websocketRouter

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultBufferSize     int = 1024
	defaultMsgDepth       int = 32
	defaultByteBufferSize int = 1024
	defaultSubProtoCount  int = 4
	errThreshHold         int = 3 //how many sequential errors can we consume before exit

	subProtoNegotiationDeadline time.Duration = time.Millisecond * 2000
)

var (
	ErrSubProtocolsRequested = errors.New("No Subprotocols Requested")
	ErrSubProtoClosed        = errors.New("SubProtoConn closed")
	ErrSubProtoNotFound      = errors.New("Subprotocol not found")
	ErrServerClosed          = errors.New("SubProtoServer closed")
	ErrAlreadyRunning        = errors.New("Already Running")
	ACK_RESP                 = SubProtocolsResp{`ACK`}
	ERR_RESP                 = SubProtocolsResp{`ERROR`}
)

type subProtoSendMsg struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type subProtoRecvMsg struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// UnkProtoMsg represents a subprotocol packet with an uknown type and unknown structure.
// This type is useful for handling erroneous messages.
type UnkProtoMsg struct {
	Type string
	Data json.RawMessage
}

// SubProtocolsReq is used to request websocket subprotocol channels during intialization.
type SubProtocolsReq struct {
	Subs []string //the string IDs of subprotocols to be used
}

// SubProtocolsResp defines the structure that a websocket router uses when responding to a request to initalize subprotocols.
type SubProtocolsResp struct {
	Resp string
}

type subParentRouter interface {
	writeProtoJSON(proto string, obj interface{}) error
}

// readDeadLine is a simple wrapper around a websocket connection that allows for recieving an object with a timeout.
func readDeadLine(conn *websocket.Conn, dur time.Duration, obj interface{}) error {
	var err error
	err = conn.SetReadDeadline(time.Now().Add(dur))
	if err != nil {
		return err
	}
	//always remove the read deadline
	defer conn.SetReadDeadline(time.Time{})
	err = conn.ReadJSON(obj)
	if err != nil {
		return err
	}
	return nil
}

// writeDeadLine is a simple wrapper around a websocket connection that allows for sending an object with a timeout.
func writeDeadLine(conn *websocket.Conn, dur time.Duration, obj interface{}) error {
	var err error
	err = conn.SetWriteDeadline(time.Now().Add(dur))
	if err != nil {
		return err
	}
	//always remove the read deadline
	defer conn.SetWriteDeadline(time.Time{})
	err = conn.WriteJSON(obj)
	if err != nil {
		return err
	}
	return nil
}
