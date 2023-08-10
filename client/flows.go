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

	"github.com/gravwell/gravwell/v3/client/types"
)

// GetFlowhList returns flows the user has access to.
func (c *Client) GetFlowList() ([]types.ScheduledSearch, error) {
	var searches []types.ScheduledSearch
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
func (c *Client) CreateFlow(name, description, schedule, flow string, groups []int32) (int32, error) {
	ss := types.ScheduledSearch{
		Groups:        groups,
		Name:          name,
		Description:   description,
		Schedule:      schedule,
		ScheduledType: types.ScheduledTypeFlow,
		Flow:          flow,
	}
	var resp int32
	if err := c.postStaticURL(flowUrl(), ss, &resp); err != nil {
		return 0, err
	}
	return resp, nil
}

// UpdateFlowResults is used to update the flow after it has been
// run. It only updates the LastRun, LastRunDuration, LastSearchIDs,
// and LastError fields.
func (c *Client) UpdateFlowResults(ss types.ScheduledSearch) error {
	return c.putStaticURL(flowResultsIdUrl(ss.ID), ss)
}

// UpdateFlow is used to modify an existing flow.
func (c *Client) UpdateFlow(ss types.ScheduledSearch) error {
	return c.putStaticURL(flowIdUrl(ss.ID), ss)
}

// DeleteFlow removes the specified flow.
func (c *Client) DeleteFlow(id int32) error {
	return c.deleteStaticURL(flowIdUrl(id), nil)
}

// GetFlow returns the flow with the given ID.
func (c *Client) GetFlow(id int32) (types.ScheduledSearch, error) {
	var search types.ScheduledSearch
	err := c.getStaticURL(flowIdUrl(id), &search)
	return search, err
}

// ClearFlowError clears the error field on the specified scheduled search.
func (c *Client) ClearFlowError(id int32) error {
	return c.deleteStaticURL(flowErrorIdUrl(id), nil)
}

// ClearFlowState clears state variables on the specified scheduled search.
func (c *Client) ClearFlowState(id int32) error {
	return c.deleteStaticURL(flowStateIdUrl(id), nil)
}

// ParseFlow asks the API to check a flow.
// If there is no error, outputPayloads will be a map containing the outputs
// of each node, keyed by the node ID.
func (c *Client) ParseFlow(flow string) (outputPayloads map[int]map[string]interface{}, err error) {
	var resp types.FlowParseResponse
	req := types.FlowParseRequest{
		Flow: flow,
	}
	if err = c.methodStaticPushURL(http.MethodPut, flowParseUrl(), req, &resp); err != nil {
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
		DebugEvent: event,
		Flow:       flow,
	}
	if err = c.methodStaticPushURL(http.MethodPut, flowParseUrl(), req, &resp); err != nil {
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
