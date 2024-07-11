/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json"
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
	// The actions the user is allowed to take on this definition.
	// Derived by the backend when requested by the user; any
	// value sent in a request will be ignored.
	Can Actions `json:"Can"`

	// A list of flows which will be run when alerts are generated.
	Consumers []AlertConsumer `json:"Consumers"`

	Description string `json:"Description"`

	Disabled bool `json:"Disabled"`

	// A list of things which create alerts (currently only scheduled searches).
	Dispatchers []AlertDispatcher `json:"Dispatchers"`

	GIDs []int32 `json:"GIDs"`

	GUID uuid.UUID `json:"GUID"`

	Global bool `json:"Global"`

	IngestBlocked bool `json:"IngestBlocked"`

	Labels []string `json:"Labels"`

	LastUpdated time.Time `json:"LastUpdated"`

	// Maximum number of events allowed per firing of the alert. This is
	// intended as a safety valve to avoid thousands of emails. If zero,
	// a (low) default value will be used.
	MaxEvents int `json:"MaxEvents"`

	Name string `json:"Name"`

	// How long, in seconds, we should save searches which trigger this alert.
	SaveSearchDuration int32 `json:"SaveSearchDuration"`

	// Whether or not searches which trigger the alert should be saved
	SaveSearchEnabled bool `json:"SaveSearchEnabled"`

	// A JSON schema describing the expected fields in the alerts.
	Schemas AlertSchemas `json:"Schemas"`

	// The tag into which alerts will be ingested
	TargetTag string `json:"TargetTag"`

	ThingUUID uuid.UUID `json:"ThingUUID"`

	// The owner of the Alert
	UID int32 `json:"UID"`

	// Arbitrary user-defined metadata which will be injected into the events
	UserMetadata map[string]interface{} `json:"UserMetadata"`

	// Sharing rules for this alert.
	WriteAccess Access `json:"WriteAccess"`
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
	Simple []AlertSchemasSimpleItem

	// A schema derived from an OCSF spec.
	OCSF AlertSchemasOcsf

	// A user-provided JSON schema.
	JSON map[string]interface{}

	ActiveSchema string
}

// AlertSchemasSimpleItem defines a single item in a Simple schema
type AlertSchemasSimpleItem struct {
	Name string

	Type string
}

// AlertSchemasOcsf defines an OCSF schema to use.
type AlertSchemasOcsf struct {
	EventClass string

	Extensions []string

	Profiles []string
}

// AlertDispatcherValidateRequest - Request to validate the given dispatcher against a schema. Populate the Dispatcher field to refer to an existing scheduled search, or set QueryString to test a query string
type AlertDispatcherValidateRequest struct {
	Dispatcher AlertDispatcher

	QueryString string

	Schema AlertSchemas
}

// AlertDispatcherValidateError - Describes a failed validation item for a dispatcher
type AlertDispatcherValidateError struct {

	// The path that led to the error
	Path string

	InvalidValue *interface{}

	// Human-friendly information as to why the item failed
	Message string
}

// AlertDispatcherValidateResponse - Indicates which, if any, fields the given dispatcher failed to provide.
type AlertDispatcherValidateResponse struct {

	// If true, the dispatcher generates all required fields in the schema.
	Valid bool

	// Names of fields which were missing.
	ValidationErrors []AlertDispatcherValidateError
}

// AlertConsumerValidateRequest - Request to validate the given consumer for use with an alert
type AlertConsumerValidateRequest struct {
	Consumer AlertConsumer

	Alert AlertDefinition
}

// AlertConsumerValidateResponse - Indicates whether a consumer is valid for a given alert or not.
type AlertConsumerValidateResponse struct {
	Valid bool

	Error string
}

func (alert *AlertDefinition) JSONMetadata() (json.RawMessage, error) {
	st := &struct {
		UUID        string
		Name        string
		Description string
	}{
		UUID:        alert.GUID.String(),
		Name:        alert.Name,
		Description: alert.Description,
	}
	b, err := json.Marshal(st)
	return json.RawMessage(b), err
}

// FindMostRelevantAutomation resolves the appropriate ScheduledSearch
// automation (scheduled search, script, or flow) for the given user
// based on the specified GUID.
func FindMostRelevantAutomation(ud UserDetails, guid uuid.UUID, automations []ScheduledSearch) (result ScheduledSearch, ok bool) {
	var adminHit bool
	var adminHitSearch ScheduledSearch
	for _, ss := range automations {
		if ss.GUID != guid {
			continue
		}
		//allow if the ownership of both match, OR the user is an admin
		if ss.Owner == ud.UID {
			ok = true
			result = ss
			return
		}
		//check if any of the gids match
		for i := range ss.Groups {
			if ud.InGroup(ss.Groups[i]) {
				// Found one shared with a group the user is in, but we don't
				// want to return it in case there's another one *owned* by the user.
				ok = true
				result = ss
			}
		}
		for i := range ss.WriteAccess.GIDs {
			if ud.InGroup(ss.WriteAccess.GIDs[i]) {
				// Found one shared with a group the user is in, but we don't
				// want to return it in case there's another one *owned* by the user.
				ok = true
				result = ss
			}
		}

		if !ok && (ss.Global || ss.WriteAccess.Global) {
			// If it's a global search, and we haven't found a match for the
			// group or the owner yet, it's a candidate.
			ok = true
			result = ss
		} else if !ok && ud.Admin == true {
			//no global
			// If it's a global search, and we haven't found a match for the
			// group or the owner yet, it's a candidate, but we don't want to override a global hit, so do some more dancing
			adminHit = true
			adminHitSearch = ss
		}
	}

	if !ok && adminHit {
		//nothing else hit but we got an admin hit, so say everything is OK
		ok = true
		result = adminHitSearch
	}
	return

}
