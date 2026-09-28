/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

const (
	ACTIONABLE_COMMAND_QUERY       ActionableCommandType = "query"
	ACTIONABLE_COMMAND_TEMPLATE    ActionableCommandType = "template"
	ACTIONABLE_COMMAND_DASHBOARD   ActionableCommandType = "dashboard"
	ACTIONABLE_COMMAND_SAVED_QUERY ActionableCommandType = "saved_query"
	ACTIONABLE_COMMAND_URL         ActionableCommandType = "url"
)

type ActionableCommandType string

// Actionable defines things the GUI can do to text displayed on the
// UI. Formerly known as Pivot.
type Actionable struct {
	CommonFields

	Contents ActionableContent
	Disabled bool
}

// ActionablePatch is the type used to request an update to an existing Actionable.
type ActionablePatch struct {
	CommonFieldsPatch
	Contents Optional[ActionableContent] `json:",omitzero"`
	Disabled Optional[bool]              `json:",omitzero"`
}

// ToPatch converts a into an ActionablePatch with every field set.
func (a Actionable) ToPatch() ActionablePatch {
	return ActionablePatch{
		CommonFieldsPatch: a.CommonFields.ToPatch(),
		Contents:          NewOptional(a.Contents),
		Disabled:          NewOptional(a.Disabled),
	}
}

// ActionableContent defines the content of an actionable (pivot),
// including its menu label, triggers, and actions.
type ActionableContent struct {
	MenuLabel string
	Actions   []ActionableAction  `json:",omitempty"`
	Triggers  []ActionableTrigger `json:",omitempty"`
}

// ActionableTrigger defines a pattern that activates an actionable.
type ActionableTrigger struct {
	// Pattern is a JS regex to match against
	Pattern string
	// ActivatesOn is either "always" or "selection". A value of
	// "selection" indicates the trigger will only appear when text has
	// been selected in-browser.
	ActivatesOn string
	Disabled    bool
}

// ActionableAction defines an action that can be performed when an actionable
// is triggered. It is a flattened representation of the API's discriminated
// union of action types (QueryAction, TemplateAction, SavedQueryAction,
// DashboardAction, URLAction); Type indicates which fields are relevant.
type ActionableAction struct {
	Type        ActionableCommandType
	Name        string
	Description string

	Query        string `json:",omitempty"` // query
	TemplateID   string `json:",omitempty"` // template
	SavedQueryID string `json:",omitempty"` // saved_query
	DashboardID  string `json:",omitempty"` // dashboard

	// Variable is the template variable that will be filled with the
	// trigger text. Used by template and dashboard actions.
	Variable string `json:",omitempty"`

	// TriggerPlaceholder is the string within the query/URL to be
	// replaced with the trigger text. Used by query and url actions.
	TriggerPlaceholder string `json:",omitempty"`

	// The following fields are only used by url actions.
	TemplateURL       string                  `json:",omitempty"`
	OpenInModal       bool                    `json:",omitempty"`
	ModalWidthPercent float64                 `json:",omitempty"`
	NoValueUrlEncode  bool                    `json:",omitempty"`
	Start             *ActionableTimeVariable `json:",omitempty"`
	End               *ActionableTimeVariable `json:",omitempty"`
}

// ActionableTimeVariable describes time-range options for a url
// action's start or end. Type is either "unix" or "string"; Format
// only applies when Type is "string". Placeholder is the string that
// will be replaced with the timestamp, e.g. `_START_` or `_END_`.
type ActionableTimeVariable struct {
	Type        string
	Format      string `json:",omitempty"`
	Placeholder string
}

type ActionableListResponse struct {
	BaseListResponse
	Results []Actionable
}
