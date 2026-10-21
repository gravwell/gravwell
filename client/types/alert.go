/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

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
	ALERTDISPATCHERTYPE_SCHEDULEDSEARCH AlertDispatcherType = "scheduled_search"
)

// Alert - A Gravwell Alert specification
type Alert struct {
	CommonFields

	Disabled bool

	// A list of flows which will be run when alerts are generated.
	Consumers []AlertConsumer

	// A list of things which create alerts (currently only scheduled searches).
	Dispatchers []AlertDispatcher

	IngestBlocked bool

	// Maximum number of events allowed per firing of the alert. This is
	// intended as a safety valve to avoid thousands of emails. If zero,
	// a (low) default value will be used.
	MaxEvents int

	// How long, in seconds, we should save searches which trigger this alert.
	SaveSearchDuration int32

	// Whether or not searches which trigger the alert should be saved
	SaveSearchEnabled bool

	// A JSON schema describing the expected fields in the alerts.
	Schemas AlertSchemas

	// The tag into which alerts will be ingested
	TargetTag string

	// Arbitrary user-defined metadata which will be injected into the events
	UserMetadata map[string]interface{}
}

// AlertPatch is the type used to request an update to an existing Alert.
type AlertPatch struct {
	CommonFieldsPatch
	Consumers          Optional[[]AlertConsumer]        `json:",omitzero"`
	Disabled           Optional[bool]                   `json:",omitzero"`
	Dispatchers        Optional[[]AlertDispatcher]      `json:",omitzero"`
	MaxEvents          Optional[int]                    `json:",omitzero"`
	SaveSearchDuration Optional[int32]                  `json:",omitzero"`
	SaveSearchEnabled  Optional[bool]                   `json:",omitzero"`
	Schemas            Optional[AlertSchemas]           `json:",omitzero"`
	TargetTag          Optional[string]                 `json:",omitzero"`
	UserMetadata       Optional[map[string]interface{}] `json:",omitzero"`
}

// ToPatch converts a into an AlertPatch with every field set.
func (a Alert) ToPatch() AlertPatch {
	return AlertPatch{
		CommonFieldsPatch:  a.CommonFields.ToPatch(),
		Disabled:           NewOptional(a.Disabled),
		Consumers:          NewOptional(a.Consumers),
		Dispatchers:        NewOptional(a.Dispatchers),
		MaxEvents:          NewOptional(a.MaxEvents),
		SaveSearchDuration: NewOptional(a.SaveSearchDuration),
		SaveSearchEnabled:  NewOptional(a.SaveSearchEnabled),
		Schemas:            NewOptional(a.Schemas),
		TargetTag:          NewOptional(a.TargetTag),
		UserMetadata:       NewOptional(a.UserMetadata),
	}
}

// AlertConsumer - Something which consumes alerts.
type AlertConsumer struct {
	ID string

	Type AlertConsumerType
}

// AlertDispatcher - Something which creates alerts.
type AlertDispatcher struct {
	ID string

	Type AlertDispatcherType
}

// AlertSchemas contains schema definitions for an alert and selects which one is to be used.
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

	Alert Alert
}

// AlertConsumerValidateResponse - Indicates whether a consumer is valid for a given alert or not.
type AlertConsumerValidateResponse struct {
	Valid bool

	Error string
}

type AlertListResponse struct {
	BaseListResponse
	Results []Alert
}

// FindMostRelevantAutomation resolves the appropriate automation
// (scheduled search, script, or flow) for the given user based on the
// specified GUID.
// func FindMostRelevantAutomation(ud User, guid uuid.UUID, automations []ScheduledSearch) (result ScheduledSearch, ok bool) {
// 	var adminHit bool
// 	var adminHitSearch ScheduledSearch
// 	for _, ss := range automations {
// 		if ss.GUID != guid {
// 			continue
// 		}
// 		//allow if the ownership of both match, OR the user is an admin
// 		if ss.Owner == ud.ID {
// 			ok = true
// 			result = ss
// 			return
// 		}
// 		//check if any of the gids match
// 		for i := range ss.Groups {
// 			if ud.IsGroupMember(ss.Groups[i]) {
// 				// Found one shared with a group the user is in, but we don't
// 				// want to return it in case there's another one *owned* by the user.
// 				ok = true
// 				result = ss
// 			}
// 		}
// 		for i := range ss.WriteAccess.GIDs {
// 			if ud.IsGroupMember(ss.WriteAccess.GIDs[i]) {
// 				// Found one shared with a group the user is in, but we don't
// 				// want to return it in case there's another one *owned* by the user.
// 				ok = true
// 				result = ss
// 			}
// 		}

// 		if !ok && (ss.Global || ss.WriteAccess.Global) {
// 			// If it's a global search, and we haven't found a match for the
// 			// group or the owner yet, it's a candidate.
// 			ok = true
// 			result = ss
// 		} else if !ok && ud.Admin {
// 			//no global
// 			// If it's a global search, and we haven't found a match for the
// 			// group or the owner yet, it's a candidate, but we don't want to override a global hit, so do some more dancing
// 			adminHit = true
// 			adminHitSearch = ss
// 		}
// 	}

// 	if !ok && adminHit {
// 		//nothing else hit but we got an admin hit, so say everything is OK
// 		ok = true
// 		result = adminHitSearch
// 	}
// 	return

// }
