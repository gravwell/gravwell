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
	Schemas AlertSchemas `json:"Schemas"`

	// The tag into which alerts will be ingested
	TargetTag string `json:"TargetTag"`

	ThingUUID uuid.UUID `json:"ThingUUID"`

	// The owner of the Alert
	UID int32 `json:"UID"`

	// Arbitrary user-defined metadata which will be injected into the events
	UserMetadata map[string]interface{} `json:"UserMetadata"`
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

// AlertSchema - Contains schema definitions for an alert and selects which one is to be used.
type AlertSchemas struct {

	// The "simple" schema, if any is defined.
	Simple []AlertSchemasSimpleItem `json:"Simple,omitempty"`

	// A schema derived from an OCSF spec.
	OCSF AlertSchemasOcsf `json:"OCSF,omitempty"`

	// A user-provided JSON schema.
	JSON map[string]interface{} `json:"JSON,omitempty"`

	ActiveSchema string `json:"ActiveSchema"`
}

// AlertSchemasSimpleItem defines a single item in a Simple schema
type AlertSchemasSimpleItem struct {
	Name string `json:"name,omitempty"`

	Type string `json:"type,omitempty"`
}

// AlertSchemasOcsf defines an OCSF schema to use.
type AlertSchemasOcsf struct {
	EventClass string `json:"EventClass"`

	Extensions []string `json:"Extensions"`

	Profiles []string `json:"Profiles"`
}

// AlertDispatcherValidateRequest - Request to validate the given dispatcher against a schema. Populate the Dispatcher field to refer to an existing scheduled search, or set QueryString to test a query string
type AlertDispatcherValidateRequest struct {
	Dispatcher AlertDispatcher `json:"Dispatcher,omitempty"`

	QueryString string `json:"QueryString,omitempty"`

	Schema AlertSchemas `json:"Schema"`
}

// AlertDispatcherValidateError - Describes a failed validation item for a dispatcher
type AlertDispatcherValidateError struct {

	// The path that led to the error
	Path string `json:"Path,omitempty"`

	InvalidValue *interface{} `json:"InvalidValue,omitempty"`

	// Human-friendly information as to why the item failed
	Message string `json:"Message,omitempty"`
}

// AlertDispatcherValidateResponse - Indicates which, if any, fields the given dispatcher failed to provide.
type AlertDispatcherValidateResponse struct {

	// If true, the dispatcher generates all required fields in the schema.
	Valid bool `json:"Valid,omitempty"`

	// Names of fields which were missing.
	ValidationErrors []AlertDispatcherValidateError `json:"ValidationErrors,omitempty"`
}

// AlertConsumerValidateRequest - Request to validate the given consumer for use with an alert
type AlertConsumerValidateRequest struct {
	Consumer AlertConsumer `json:"Consumer"`

	Alert AlertDefinition `json:"Alert"`
}

// AlertConsumerValidateResponse - Indicates whether a consumer is valid for a given alert or not.
type AlertConsumerValidateResponse struct {
	Valid bool `json:"Valid,omitempty"`

	Error string `json:"Error,omitempty"`
}

// Event is the type that actually gets ingested
type Event struct {
	Metadata EventMetadata
	Contents map[string]interface{}
}

// EventMetadata tells us about the owner of this event definition and who created the event.
type EventMetadata struct {
	UID          int32
	Username     string
	Created      time.Time
	AlertID      uuid.UUID // ThingUUID
	AlertName    string
	AlertLabels  []string
	Dispatcher   EventDispatcherInfo
	UserMetadata map[string]interface{} // this is arbitrary stuff that the user attached to the alert def.
}

// EventDispatcherInfo gives extended information about the thing which triggered the alert / created the event
type EventDispatcherInfo struct {
	Type         AlertDispatcherType
	ID           string
	Name         string   `json:",omitempty"`
	SearchID     string   `json:",omitempty"`
	Labels       []string // any labels attached to this dispatcher
	EventCount   int      // number of events that were generated by the dispatcher
	EventsElided bool     // true if we had to drop events because there were too many results
}

func BuildEventMetadata(created time.Time, ud UserDetails, alertDef AlertDefinition, dispatcher EventDispatcherInfo) EventMetadata {
	meta := EventMetadata{
		UID:          ud.UID,
		Username:     ud.User,
		Created:      created,
		AlertID:      alertDef.ThingUUID,
		AlertLabels:  alertDef.Labels,
		AlertName:    alertDef.Name,
		Dispatcher:   dispatcher,
		UserMetadata: alertDef.UserMetadata,
	}
	return meta
}
