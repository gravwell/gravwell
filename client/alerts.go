/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
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

// CreateAlert creates a new alert.
func (c *Client) CreateAlert(def types.Alert) (result types.Alert, err error) {
	err = c.methodStaticPushURL(http.MethodPost, alertsUrl(), def, &result, nil, nil)
	return
}

// ListAlerts returns a list of alerts the user has access to.
func (c *Client) ListAlerts(opts *types.QueryOptions) (result types.AlertListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(ALERTS_LIST_URL, opts, &result)
	return
}

// ListAllAlerts (admin-only) returns all alerts on the system.
func (c *Client) ListAllAlerts(opts *types.QueryOptions) (result types.AlertListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true
	err = c.postStaticURL(ALERTS_LIST_URL, opts, &result)
	return
}

// GetAlertsByDispatcher returns a list of alerts who refer to the specified dispatcher.
// dispatcherID should be the *ID* of the a scheduled search, not the *GUID*.
// Basically, this lets you ask: which alerts will be invoked by *this specific scheduled search*.
// func (c *Client) GetAlertsByDispatcher(dispatcherID string, dispatcherType types.AlertDispatcherType) (result []types.Alert, err error) {
// 	c.qm.set("dispatcher", dispatcherID)
// 	c.qm.set("type", string(dispatcherType))
// 	err = c.getStaticURL(alertsUrl(), &result)
// 	c.qm.remove("type")
// 	c.qm.remove("dispatcher")
// 	return
// }

// GetAlertsByConsumer returns a list of alerts who refer to the specified consumer.
// consumerID should be the *ID* of the a flow, not the *GUID*.
// Basically, this lets you ask: which alerts will launch *this specific flow*.
// func (c *Client) GetAlertsByConsumer(consumerID string, consumerType types.AlertConsumerType) (result []types.Alert, err error) {
// 	c.qm.set("consumer", consumerID)
// 	c.qm.set("type", string(consumerType))
// 	err = c.getStaticURL(alertsUrl(), &result)
// 	c.qm.remove("type")
// 	c.qm.remove("consumer")
// 	return
// }

// GetAlert returns the definition for a specific alert.
func (c *Client) GetAlert(id string) (result types.Alert, err error) {
	err = c.getStaticURL(alertsIdUrl(id), &result)
	return
}

// GetAlertEx returns the definition for a specific alert, applying
// parameters from QueryOptions if appropriate. Currently only
// IncludeDeleted is supported.
func (c *Client) GetAlertEx(id string, opts *types.QueryOptions) (result types.Alert, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.getStaticURL(alertsIdUrl(id), &result, ezParam("include_deleted", opts.IncludeDeleted))
	return
}

// UpdateAlert modifies an alert. Make sure to have ID set, as this is used to resolve
// the appropriate alert to modify.
func (c *Client) UpdateAlert(def types.Alert) (result types.Alert, err error) {
	err = c.methodStaticPushURL(http.MethodPut, alertsIdUrl(def.ID), def, &result, nil, nil)
	return
}

// DeleteAlert marks an alert as deleted.
func (c *Client) DeleteAlert(id string) (err error) {
	err = c.deleteStaticURL(alertsIdUrl(id), nil)
	return
}

// PurgeAlert deletes an alert completely from the database
func (c *Client) PurgeAlert(id string) (err error) {
	err = c.deleteStaticURL(alertsIdUrl(id), nil, ezParam("purge", "true"))
	return
}

// GetAlertSampleEvent asks the webserver to generate a sample event for the given alert.
func (c *Client) GetAlertSampleEvent(id string) (result types.Event, err error) {
	err = c.getStaticURL(alertsIdSampleEventUrl(id), &result)
	return
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
	err = c.methodStaticPushURL(http.MethodPost, alertsValidateDispatcherUrl(), req, &resp, nil, nil)
	return

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
	err = c.methodStaticPushURL(http.MethodPost, alertsValidateConsumerUrl(), req, &resp, nil, nil)
	return

}

// CleanupAlerts (admin-only) purges all deleted alerts for all users.
func (c *Client) CleanupAlerts() error {
	return c.deleteStaticURL(ALERTS_URL, nil)
}
