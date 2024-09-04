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

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
)

// NewAlert creates a new alert.
func (c *Client) NewAlert(def types.AlertDefinition) (result types.AlertDefinition, err error) {
	err = c.methodStaticPushURL(http.MethodPost, alertsUrl(), def, &result, nil, nil)
	return
}

// GetAlerts returns a list of alerts the user has access to.
// As admin, set the admin flag (c.SetAdminMode) to get a list of all alerts
// on the system.
func (c *Client) GetAlerts() (result []types.AlertDefinition, err error) {
	err = c.getStaticURL(alertsUrl(), &result)
	return
}

// GetAlertsByDispatcher returns a list of alerts who refer to the specified dispatcher.
// dispatcherID should be the *ID* of the a scheduled search, not the *GUID*.
// Basically, this lets you ask: which alerts will be invoked by *this specific scheduled search*.
func (c *Client) GetAlertsByDispatcher(dispatcherID string, dispatcherType types.AlertDispatcherType) (result []types.AlertDefinition, err error) {
	c.qm.set("dispatcher", dispatcherID)
	c.qm.set("type", string(dispatcherType))
	err = c.getStaticURL(alertsUrl(), &result)
	c.qm.remove("type")
	c.qm.remove("dispatcher")
	return
}

// GetAlertsByConsumer returns a list of alerts who refer to the specified consumer.
// consumerID should be the *ID* of the a flow, not the *GUID*.
// Basically, this lets you ask: which alerts will launch *this specific flow*.
func (c *Client) GetAlertsByConsumer(consumerID string, consumerType types.AlertConsumerType) (result []types.AlertDefinition, err error) {
	c.qm.set("consumer", consumerID)
	c.qm.set("type", string(consumerType))
	err = c.getStaticURL(alertsUrl(), &result)
	c.qm.remove("type")
	c.qm.remove("consumer")
	return
}

// GetAlert returns the definition for a specific alert. The id passed can be
// either a ThingUUID, which will always return a specific alert, or a GUID, in
// which case the webserver will attempt to resolve the "most appropriate" alert
// with that GUID.
func (c *Client) GetAlert(id uuid.UUID) (result types.AlertDefinition, err error) {
	err = c.getStaticURL(alertsIdUrl(id), &result)
	return
}

// UpdateAlert modifies an alert. Make sure to have ThingUUID set, as this is used to resolve
// the appropriate alert to modify.
func (c *Client) UpdateAlert(def types.AlertDefinition) (result types.AlertDefinition, err error) {
	err = c.methodStaticPushURL(http.MethodPut, alertsIdUrl(def.ThingUUID), def, &result, nil, nil)
	return
}

// DeleteAlert deletes an alert. The id must be the ThingUUID, for precision.
func (c *Client) DeleteAlert(id uuid.UUID) (err error) {
	err = c.deleteStaticURL(alertsIdUrl(id), nil)
	return
}

// GetAlertSampleEvent asks the webserver to generate a sample event for the given alert.
func (c *Client) GetAlertSampleEvent(id uuid.UUID) (result types.Event, err error) {
	err = c.getStaticURL(alertsIdSampleEventUrl(id), &result)
	return
}

// ValidateAlertScheduledSearchDispatcher validates an existing scheduled search against
// a given schema.
func (c *Client) ValidateAlertScheduledSearchDispatcher(ssearchID uuid.UUID, schema types.AlertSchemas) (resp types.AlertDispatcherValidateResponse, err error) {
	// build the request
	req := types.AlertDispatcherValidateRequest{
		Dispatcher: types.AlertDispatcher{
			Type: types.ALERTDISPATCHERTYPE_SCHEDULEDSEARCH,
			ID:   ssearchID.String(),
		},
		Schema: schema,
	}
	err = c.methodStaticPushURL(http.MethodPost, alertsValidateDispatcherUrl(), req, &resp, nil, nil)
	return

}

// ValidateAlertFlowConsumer validates an existing flow against
// a given alert, making sure it does not consume any fields not
// provided by the schema.
func (c *Client) ValidateAlertFlowConsumer(flowID uuid.UUID, alert types.AlertDefinition) (resp types.AlertConsumerValidateResponse, err error) {
	// build the request
	req := types.AlertConsumerValidateRequest{
		Consumer: types.AlertConsumer{
			Type: types.ALERTCONSUMERTYPE_FLOW,
			ID:   flowID.String(),
		},
		Alert: alert,
	}
	err = c.methodStaticPushURL(http.MethodPost, alertsValidateConsumerUrl(), req, &resp, nil, nil)
	return

}
