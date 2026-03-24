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

// ListFlows returns flows the user has access to.
func (c *Client) ListFlows(opts *types.QueryOptions) (flows types.FlowListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	if err = c.postStaticURL(FLOW_LIST_URL, opts, &flows); err != nil {
		return
	}
	return
}

// ListAllFlows returns all flows on the system (for admins).
func (c *Client) ListAllFlows(opts *types.QueryOptions) (flows types.FlowListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	if err = c.postStaticURL(FLOW_LIST_URL, opts, &flows); err != nil {
		return
	}
	return
}

// GetFlow returns the flow with the given ID.
func (c *Client) GetFlow(id string) (types.Flow, error) {
	var flow types.Flow
	err := c.getStaticURL(flowIdUrl(id), &flow)
	return flow, err
}

// GetFlowEx returns a particular flow. If the QueryOptions arg is
// not nil, applicable parameters (currently only IncludeDeleted) will
// be applied to the query.
func (c *Client) GetFlowEx(id string, opts *types.QueryOptions) (types.Flow, error) {
	var flow types.Flow
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err := c.getStaticURL(flowIdUrl(id), &flow, ezParam("include_deleted", opts.IncludeDeleted))
	return flow, err
}

// DeleteFlow removes the specified flow.
func (c *Client) DeleteFlow(id string) error {
	return c.deleteStaticURL(flowIdUrl(id), nil)
}

// PurgeFlow permanently removes the specified flow.
func (c *Client) PurgeFlow(id string) error {
	return c.deleteStaticURL(flowIdUrl(id), nil, ezParam("purge", "true"))
}

// CreateFlow makes a new flow.
func (c *Client) CreateFlow(spec types.Flow) (result types.Flow, err error) {
	err = c.postStaticURL(flowUrl(), spec, &result)
	return
}

// UpdateFlow is used to modify an existing flow.
func (c *Client) UpdateFlow(ss types.Flow) error {
	return c.putStaticURL(flowIdUrl(ss.ID), ss)
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

// ReportFlowResults uploads a set of results for the flow with the specified ID.
func (c *Client) ReportFlowResults(id string, results types.FlowResults) error {
	return c.postStaticURL(flowResultsIdUrl(id), results, nil)
}

// GetFlowResults retrieves the most recent results for the specified flow
func (c *Client) GetFlowResults(id string) (results types.FlowResults, err error) {
	err = c.getStaticURL(flowResultsIdUrl(id), &results)
	return
}

// ClearFlowResults deletes all results for the specified flow
func (c *Client) ClearFlowResults(id string) error {
	return c.deleteStaticURL(flowResultsIdUrl(id), nil)
}

// DebugFlow schedules an immediate execution of the specified flow.
func (c *Client) DebugFlow(id string, opts types.AutomationDebugRequest) error {
	return c.postStaticURL(flowDebugIdUrl(id), opts, nil)
}

// CancelFlow cancels any active run of the specified flow.
func (c *Client) CancelFlow(id string) error {
	return c.deleteStaticURL(flowCancelIdUrl(id), nil)
}
