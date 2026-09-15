/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"github.com/gravwell/gravwell/v4/client/types"
)

// CreateAlert creates a new alert.
func (c *Client) CreateAlert(def types.Alert) (result types.Alert, err error) {
	return c.post[types.Alert, types.Alert](alertsUrl(), &def)
}

// ListAlerts returns a list of alerts the user has access to.
func (c *Client) ListAlerts(opts *types.QueryOptions) (result types.AlertListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	return c.post[types.QueryOptions, types.AlertListResponse](ALERTS_LIST_URL, opts)
}

// ListAllAlerts (admin-only) returns all alerts on the system.
func (c *Client) ListAllAlerts(opts *types.QueryOptions) (result types.AlertListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true
	return c.post[types.QueryOptions, types.AlertListResponse](ALERTS_LIST_URL, opts)
}

// GetAlert returns the definition for a specific alert.
func (c *Client) GetAlert(id string) (result types.Alert, err error) {
	return c.GetAlertEx(id, GetOptions{})
}

// GetAlertEx returns the definition for a specific alert, modified by opts.
func (c *Client) GetAlertEx(id string, opts GetOptions) (result types.Alert, err error) {
	return c.get[types.Alert](alertsIdUrl(id), opts.params()...)
}

// UpdateAlert modifies an existing alert and returns the complete, updated struct.
func (c *Client) UpdateAlert(ID string, p types.AlertPatch) (updated types.Alert, err error) {
	if ID == "" {
		return types.Alert{}, ErrEmptyID
	}
	return c.patch[types.AlertPatch, types.Alert](alertsIdUrl(ID), p)
}

// DeleteAlert marks an alert as deleted.
func (c *Client) DeleteAlert(id string) (err error) {
	return c.delete(alertsIdUrl(id), false)
}

// PurgeAlert deletes an alert completely from the database
func (c *Client) PurgeAlert(id string) (err error) {
	return c.delete(alertsIdUrl(id), true)
}

// GetAlertSampleEvent asks the webserver to generate a sample event for the given alert.
func (c *Client) GetAlertSampleEvent(id string) (result types.Event, err error) {
	return c.get[types.Event](alertsIdSampleEventUrl(id))
}

// ValidateAlertScheduledSearchDispatcher validates an existing scheduled search against
// a given schema.
func (c *Client) ValidateAlertScheduledSearchDispatcher(ssearchID string, schema types.AlertSchemas) (resp types.AlertDispatcherValidateResponse, err error) {
	// build the request
	req := types.AlertDispatcherValidateRequest{
		Dispatcher: types.AlertDispatcher{
			Type: types.ALERTDISPATCHERTYPE_SCHEDULEDSEARCH,
			ID:   ssearchID,
		},
		Schema: schema,
	}
	return c.post[types.AlertDispatcherValidateRequest, types.AlertDispatcherValidateResponse](alertsValidateDispatcherUrl(), &req)
}

// ValidateAlertFlowConsumer validates an existing flow against
// a given alert, making sure it does not consume any fields not
// provided by the schema.
func (c *Client) ValidateAlertFlowConsumer(flowID string, alert types.Alert) (resp types.AlertConsumerValidateResponse, err error) {
	// build the request
	req := types.AlertConsumerValidateRequest{
		Consumer: types.AlertConsumer{
			Type: types.ALERTCONSUMERTYPE_FLOW,
			ID:   flowID,
		},
		Alert: alert,
	}
	return c.post[types.AlertConsumerValidateRequest, types.AlertConsumerValidateResponse](alertsValidateConsumerUrl(), &req)
}

// CleanupAlerts (admin-only) purges all deleted alerts for all users.
func (c *Client) CleanupAlerts() error {
	return c.delete(ALERTS_URL, false)
}
