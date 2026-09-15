/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"net/http"

	"github.com/gravwell/gravwell/v4/client/types"
)

// ListFlows returns flows the user has access to.
func (c *Client) ListFlows(opts *types.QueryOptions) (flows types.FlowListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	return c.post[types.QueryOptions, types.FlowListResponse](FLOW_LIST_URL, opts)
}

// ListAllFlows returns all flows on the system (for admins).
func (c *Client) ListAllFlows(opts *types.QueryOptions) (flows types.FlowListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	return c.post[types.QueryOptions, types.FlowListResponse](FLOW_LIST_URL, opts)
}

// GetFlow returns the flow with the given ID.
func (c *Client) GetFlow(id string) (types.Flow, error) {
	return c.GetFlowEx(id, GetOptions{})
}

// GetFlowEx returns a particular flow, modified by opts.
func (c *Client) GetFlowEx(id string, opts GetOptions) (types.Flow, error) {
	return c.get[types.Flow](flowIdUrl(id), opts.params()...)
}

// DeleteFlow removes the specified flow.
func (c *Client) DeleteFlow(id string) error {
	return c.delete(flowIdUrl(id), false)
}

// PurgeFlow permanently removes the specified flow.
func (c *Client) PurgeFlow(id string) error {
	return c.delete(flowIdUrl(id), true)
}

// CreateFlow makes a new flow.
func (c *Client) CreateFlow(spec types.Flow) (result types.Flow, err error) {
	return c.post[types.Flow, types.Flow](flowUrl(), &spec)
}

// UpdateFlow modifies an existing flow and returns the complete, updated struct.
func (c *Client) UpdateFlow(ID string, p types.FlowPatch) (updated types.Flow, err error) {
	if ID == "" {
		return types.Flow{}, ErrEmptyID
	}
	return c.patch[types.FlowPatch, types.Flow](flowIdUrl(ID), p)
}

// ParseFlow asks the API to check a flow.  It will only return an
// error if there was a problem submitting the request; check the OK
// field of the FlowParseResponse to see if the parse succeeded or
// not.
func (c *Client) ParseFlow(flow string) (resp types.FlowParseResponse, err error) {
	req := types.FlowParseRequest{
		Flow: flow,
	}
	if err = c.methodStaticPushURL(http.MethodPut, flowParseUrl(), req, &resp, nil, nil); err != nil {
		return
	}
	return
}

// ParseReactiveFlow asks the API to check a flow as if triggered by
// an alert.  The event parameter will be injected into the initial
// payload under the name `event`.  It will only return an error if
// there was a problem submitting the request; check the OK field of
// the FlowParseResponse to see if the parse succeeded or not.
func (c *Client) ParseReactiveFlow(flow string, event types.Event) (resp types.FlowParseResponse, err error) {
	req := types.FlowParseRequest{
		DebugEvent: &event,
		Flow:       flow,
	}
	if err = c.methodStaticPushURL(http.MethodPut, flowParseUrl(), req, &resp, nil, nil); err != nil {
		return
	}
	return
}

// ReportFlowResults uploads a set of results for the flow with the specified ID.
func (c *Client) ReportFlowResults(id string, results types.FlowResults) error {
	return c.postStaticURL(flowResultsIdUrl(id), results, nil)
}

// GetFlowResults retrieves the most recent results for the specified flow
func (c *Client) GetFlowResults(id string) (results types.FlowResults, err error) {
	return c.get[types.FlowResults](flowResultsIdUrl(id))
}

// ClearFlowResults deletes all results for the specified flow
func (c *Client) ClearFlowResults(id string) error {
	return c.delete(flowResultsIdUrl(id), false)
}

// DebugFlow schedules an immediate execution of the specified flow.
func (c *Client) DebugFlow(id string, opts types.AutomationDebugRequest) error {
	return c.postStaticURL(flowDebugIdUrl(id), opts, nil)
}

// CancelFlow cancels any active run of the specified flow.
func (c *Client) CancelFlow(id string) error {
	return c.delete(flowCancelIdUrl(id), false)
}
