/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"time"

	"github.com/google/uuid"
)

// AlertConsumerType : Possible types for an Alert Consumer
type AlertConsumerType string

// List of AlertConsumerType
const (
	ALERTCONSUMERTYPE_FLOW AlertConsumerType = "flow"
)

// AlertDispatcherType : Possible types for an Alert Dispatcher
type AlertDispatcherType string

// List of AlertDispatcherType
const (
	ALERTDISPATCHERTYPE_SCHEDULEDSEARCH AlertDispatcherType = "scheduledsearch"
)

// AlertDefinition - A Gravwell Alert specification
type AlertDefinition struct {

	// A list of flows which will be run when alerts are generated.
	Consumers []AlertConsumer `json:"Consumers"`

	Description string `json:"Description"`

	// A list of things which create alerts (currently only scheduled searches).
	Dispatchers []AlertDispatcher `json:"Dispatchers"`

	GIDs []int32 `json:"GIDs"`

	GUID uuid.UUID `json:"GUID"`

	Global bool `json:"Global"`

	Labels []string `json:"Labels"`

	LastUpdated time.Time `json:"LastUpdated"`

	Name string `json:"Name"`

	// A JSON schema describing the expected fields in the alerts.
	Schema map[string]interface{} `json:"Schema"`

	// The tag into which alerts will be ingested
	TargetTag string `json:"TargetTag"`

	ThingUUID uuid.UUID `json:"ThingUUID"`

	// The owner of the Alert
	UID int32 `json:"UID"`
}

// AlertConsumer - Something which consumes alerts.
type AlertConsumer struct {
	ID string `json:"ID"`

	Type AlertConsumerType `json:"Type"`
}

// AlertDispatcher - Something which creates alerts.
type AlertDispatcher struct {
	ID string `json:"ID"`

	Type AlertDispatcherType `json:"Type"`
}
