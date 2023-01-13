/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v3/client/websocketRouter"
)

const (
	// Websocket subprotocols
	PROTO_PING   string = "ping"
	PROTO_IDX    string = "idxStats"
	PROTO_SYS    string = "sysStats"
	PROTO_DESC   string = "sysDesc"
	PROTO_IGST   string = "igstStats"
	PROTO_PONG   string = `PONG`
	PROTO_PARSE  string = `parse`
	PROTO_SEARCH string = `search`
	PROTO_ATTACH string = `attach`

	STAT_R_SIZE = 1024
	STAT_W_SIZE = 1024

	jwtWebsocketHeader string = `Sec-Websocket-Protocol`
)

type sockParams struct {
	subProtos    []string
	negSubProtos []string
	uri          string
}

func (c *Client) websocketHeaderMap() (m map[string]string) {
	m = c.hm.dump()
	m[jwtWebsocketHeader] = c.sessionData.JWT
	return
}

func (c *Client) getSockets(p sockParams) ([]*websocketRouter.SubProtoConn, *websocketRouter.SubProtoClient, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.state != STATE_AUTHED {
		return nil, nil, ErrNoLogin
	}
	spc, err := websocketRouter.NewSubProtoClient(p.uri, c.websocketHeaderMap(), STAT_R_SIZE, STAT_W_SIZE, c.enforceCert, p.negSubProtos, c.objLog)
	if err != nil {
		return nil, nil, err
	}

	//kill off all but the subproto we want
	subs, err := spc.SubProtocols()
	if err != nil {
		spc.Close()
		return nil, nil, err
	}
	for i := range subs {
		killSub := true
		//check if this sub is in the list we want
		for j := range p.subProtos {
			if subs[i] == p.subProtos[j] {
				killSub = false
				break
			}
		}
		if !killSub {
			continue
		}
		//wasn't in the list, delete it
		if err = spc.CloseSubProtoConn(subs[i]); err != nil {
			spc.Close()
			return nil, nil, err
		}
	}

	err = spc.Start()
	if err != nil {
		spc.Close()
		return nil, nil, err
	}
	var retSubProtoConns []*websocketRouter.SubProtoConn
	for i := range p.subProtos {
		subProtoConn, err := spc.GetSubProtoConn(p.subProtos[i])
		if err != nil {
			spc.Close()
			return nil, nil, err
		}
		if c.timeout > 0 {
			if err := subProtoConn.SetTimeout(c.timeout); err != nil {
				return nil, nil, err
			}
		}
		retSubProtoConns = append(retSubProtoConns, subProtoConn)
	}

	return retSubProtoConns, spc, nil

}

// GetStatSocket will connect to the websocketRouter and get the subProto client for
// the stats socket only.
func (c *Client) GetStatSocket(subProto string) (*websocketRouter.SubProtoConn, *websocketRouter.SubProtoClient, error) {
	subs := []string{PROTO_PING, PROTO_IDX, PROTO_SYS, PROTO_DESC} //PROTO_IGST is optional for the websocket
	//ensure the subprotocol being requested is provided by the server
	switch subProto {
	case PROTO_PING:
	case PROTO_IDX:
	case PROTO_IGST: //because its optional, we only add it if we request it
		subs = append(subs, PROTO_IGST)
	case PROTO_SYS:
	default:
		return nil, nil, errors.New("Invalid subprotocol")
	}

	//get our param list rolling
	params := sockParams{
		subProtos:    []string{subProto},
		negSubProtos: subs,
		uri:          fmt.Sprintf("%s://%s%s", c.wsScheme, c.serverURL.Host, WS_STAT_URL),
	}
	s, p, err := c.getSockets(params)
	if err != nil {
		return nil, nil, err
	}
	if c.timeout > 0 {
		if err := s[0].SetTimeout(c.timeout); err != nil {
			return nil, nil, err
		}
	}
	return s[0], p, nil
}

// SearchSockets wraps up several different websocket subprotocols. Depending on the
// function used to obtain the SearchSockets object, not all subprotocols may be
// populated--refer to the individual function's documentation.
type SearchSockets struct {
	Parse  *websocketRouter.SubProtoConn
	Search *websocketRouter.SubProtoConn
	Attach *websocketRouter.SubProtoConn
	Pong   *websocketRouter.SubProtoConn
	Client *websocketRouter.SubProtoClient
}

// GetSearchSockets will hit the search routing websocket page and pull back
// the parse, search, and attach subprotocols.
func (c *Client) GetSearchSockets() (*SearchSockets, error) {
	subs := []string{PROTO_PONG, PROTO_PARSE, PROTO_SEARCH, PROTO_ATTACH}

	//get our param list rolling
	params := sockParams{
		subProtos:    []string{PROTO_PARSE, PROTO_SEARCH, PROTO_ATTACH, PROTO_PONG},
		negSubProtos: subs,
		uri:          fmt.Sprintf("%s://%s%s", c.wsScheme, c.serverURL.Host, WS_SEARCH_URL),
	}
	subConns, p, err := c.getSockets(params)
	if err != nil {
		return nil, err
	}
	if len(subConns) != len(params.subProtos) {
		return nil, errors.New("Invalid response protocol count")
	}
	if c.timeout > 0 {
		for i := range subConns {
			if err := subConns[i].SetTimeout(c.timeout); err != nil {
				return nil, err
			}
		}
	}
	//the subs come back in the same order we asked for them
	return &SearchSockets{
		Parse:  subConns[0],
		Search: subConns[1],
		Attach: subConns[2],
		Pong:   subConns[3],
		Client: p,
	}, nil
}

// GetAttachSockets will hit the search routing websocket page and pull back only the attach
// socket.
func (c *Client) GetAttachSockets() (*SearchSockets, error) {
	subs := []string{PROTO_ATTACH, PROTO_PONG}

	//get our param list rolling
	params := sockParams{
		subProtos:    subs,
		negSubProtos: subs,
		uri:          fmt.Sprintf("%s://%s%s", c.wsScheme, c.serverURL.Host, WS_SEARCH_URL),
	}
	subConns, p, err := c.getSockets(params)
	if err != nil {
		return nil, err
	}
	if len(subConns) != len(params.subProtos) {
		return nil, errors.New("Invalid response protocol count")
	}
	if c.timeout > 0 {
		for i := range subConns {
			if err := subConns[i].SetTimeout(c.timeout); err != nil {
				return nil, err
			}
		}
	}
	//the subs come back in the same order we asked for them
	return &SearchSockets{
		Attach: subConns[0],
		Pong:   subConns[1],
		Client: p,
	}, nil
}
