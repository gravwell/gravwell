/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"errors"
	"net/http"

	"github.com/gravwell/gravwell/v4/client/types"
)

// GetFlowList returns flows the user has access to.
func (c *Client) GetFlowList() ([]types.Flow, error) {
	var searches []types.Flow
	if err := c.getStaticURL(flowUrl(), &searches); err != nil {
		return nil, err
	}
	return searches, nil
}

// CreateFlow makes a new flow and returns the ID. The parameters are:
//
// - name: the flow name.
//
// - description: the flow description.
//
// - schedule: a cron-format schedule on which to execute the flow.
//
// - flow: a valid JSON flow definition.
//
// - groups: an optional array of groups which should be able to access this object.
func (c *Client) CreateFlow(spec types.Flow) (types.Flow, error) {
	return types.Flow{}, nil
}

// UpdateFlow is used to modify an existing flow.
func (c *Client) UpdateFlow(ss types.Flow) error {
	return c.putStaticURL(flowIdUrl(ss.ID), ss)
}

// DeleteFlow removes the specified flow.
func (c *Client) DeleteFlow(id string) error {
	return c.deleteStaticURL(flowIdUrl(id), nil)
}

// GetFlow returns the flow with the given ID. The ID is an interface{}
// to allow the user to specify either the flow's int32 "ID" or its
// UUID "GUID" field.
func (c *Client) GetFlow(id string) (types.Flow, error) {
	var search types.Flow
	err := c.getStaticURL(flowIdUrl(id), &search)
	return search, err
}

// ParseFlow asks the API to check a flow.
// If there is no error, outputPayloads will be a map containing the outputs
// of each node, keyed by the node ID.
func (c *Client) ParseFlow(flow string) (outputPayloads map[int]map[string]interface{}, err error) {
	var resp types.FlowParseResponse
	req := types.FlowParseRequest{
		Flow: flow,
	}
	if err = c.methodStaticPushURL(http.MethodPut, flowParseUrl(), req, &resp, nil, nil); err != nil {
		return
	}

	//if the parse failed but we don't have an error, set something
	if !resp.OK {
		if len(resp.Error) == 0 {
			resp.Error = `Unknown parse error`
		}
		err = errors.New(resp.Error)
	}
	outputPayloads = resp.OutputPayloads
	return
}

// ParseReactiveFlow asks the API to check a flow as if triggered by an alert.
// The event parameter will be injected into the initial payload under the name `event`.
// If there is no error, outputPayloads will be a map containing the outputs
// of each node, keyed by the node ID.
func (c *Client) ParseReactiveFlow(flow string, event types.Event) (outputPayloads map[int]map[string]interface{}, err error) {
	var resp types.FlowParseResponse
	req := types.FlowParseRequest{
		DebugEvent: &event,
		Flow:       flow,
	}
	if err = c.methodStaticPushURL(http.MethodPut, flowParseUrl(), req, &resp, nil, nil); err != nil {
		return
	}

	//if the parse failed but we don't have an error, set something
	if !resp.OK {
		if len(resp.Error) == 0 {
			resp.Error = `Unknown parse error`
		}
		err = errors.New(resp.Error)
	}
	outputPayloads = resp.OutputPayloads
	return
}
