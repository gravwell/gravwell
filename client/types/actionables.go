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

type ActionableListResponse = ListResponse[Actionable]

// Valid values for ActionableTriggerComplexity.Severity.
const (
	ACTIONABLE_TRIGGER_SEVERITY_LOW    ActionableTriggerSeverity = "low"
	ACTIONABLE_TRIGGER_SEVERITY_MEDIUM ActionableTriggerSeverity = "medium"
	ACTIONABLE_TRIGGER_SEVERITY_HIGH   ActionableTriggerSeverity = "high"
)

// ActionableTriggerSeverity is how expensive a trigger pattern is to evaluate.
type ActionableTriggerSeverity string

// ActionableTriggerGroup is every action reachable through one unique trigger.
// Actionables that declare the same pattern with the same activation arrive as
// one group, so that a client compiles and runs the pattern once instead of
// once per actionable that happened to declare it.
type ActionableTriggerGroup struct {
	// Pattern is a JS regex to match against.
	Pattern string
	// ActivatesOn is either "always" or "selection", as on ActionableTrigger.
	ActivatesOn string
	// Complexity estimates what the pattern costs to run.
	Complexity ActionableTriggerComplexity
	// Menus holds the actions the group offers, grouped by the label they are
	// shown under and ordered by that label.
	Menus []ActionableTriggerMenu
}

// ActionableTriggerComplexity is what one pattern costs to evaluate, so that a
// client can skip, or turn off by default, the patterns that are expensive to
// run over everything it renders. It is a heuristic: a low severity is not a
// proof that a pattern is cheap.
type ActionableTriggerComplexity struct {
	// Severity is the overall estimate: "low", "medium" or "high".
	Severity ActionableTriggerSeverity
	// Backtracking reports whether the pattern uses a construct only a
	// backtracking engine can run, such as a lookaround or a backreference.
	Backtracking bool
	// Reasons describes each finding, one sentence apiece. It is empty when
	// nothing was found.
	Reasons []string
}

// ActionableTriggerMenu is the actions one menu label offers for a trigger,
// merged from every actionable that declared that trigger under that label.
type ActionableTriggerMenu struct {
	// MenuLabel is the label the actions are shown under. An actionable with no
	// menu label of its own contributes under its name.
	MenuLabel string
	// ActionableIDs names every actionable that contributed to this menu, in
	// the order they first contributed. An actionable appears here whenever it
	// declared the trigger under this label, including when every action it
	// offers was already present from another one, so these do not pair up
	// one-to-one with Actions.
	ActionableIDs []string
	// Actions is every action the contributing actionables offer, deduplicated.
	Actions []ActionableAction
}

// ActionableTriggerListOptions is what the deduplicated trigger listing takes.
// It is deliberately narrower than QueryOptions: a trigger group is not an
// asset, so there is no All to widen to and no Version to pin, and the endpoint
// rejects a request that carries either. Filters, Search, OrderBy,
// OrderDirection and IncludeDeleted are accepted and ignored, since the order
// is fixed and a group is not an asset to filter on.
type ActionableTriggerListOptions struct {
	// CursorID names the first group of the page being asked for, as a
	// CursorNext or CursorPrev from an earlier response.
	CursorID string
	// Limit is the max number of groups to return.
	// Defaults to QueryOptionsDefaultLimit and is bound by QueryOptionsMaxLimit.
	Limit int32

	// Taken and ignored, so that a caller can hand over the same options it
	// would give any other listing.
	Filters        []Filter
	IncludeDeleted bool
	OrderBy        string
	OrderDirection string
	Search         string
}

// ActionableTriggerListResponse is a page of deduplicated triggers.
type ActionableTriggerListResponse struct {
	BaseListResponse
	Results []ActionableTriggerGroup
}
