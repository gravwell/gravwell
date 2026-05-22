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

// ActionableContent defines the content of an actionable (pivot),
// including its menu label, triggers, and actions.
type ActionableContent struct {
	MenuLabel string              `json:"menuLabel"`
	Actions   []ActionableAction  `json:"actions,omitempty"`
	Triggers  []ActionableTrigger `json:"triggers,omitempty"`
}

// ActionableTrigger defines a pattern that activates an actionable.
type ActionableTrigger struct {
	// Pattern is a JS regex to match against
	Pattern   string `json:"pattern"`
	Hyperlink bool   `json:"hyperlink"`
	Disabled  bool   `json:"disabled"`
}

// ActionableAction defines an action that can be performed when an actionable is triggered.
type ActionableAction struct {
	Name             string                  `json:"name"`
	Description      string                  `json:"description"`
	Placeholder      string                  `json:"placeholder"`
	NoValueURLEncode bool                    `json:"noValueUrlEncode,omitempty"`
	Start            *ActionableTimeVariable `json:"start,omitempty"`
	End              *ActionableTimeVariable `json:"end,omitempty"`
	Command          ActionableCommand       `json:"command"`
}

// ActionableTimeVariable describes time-range options for an action's start or end.
// Type is either "timestamp" or "string".
type ActionableTimeVariable struct {
	Type        string `json:"type"`
	Format      string `json:"format"`
	Placeholder string `json:"placeholder"`
}

// ActionableCommand defines the command performed when an action is activated.
type ActionableCommand struct {
	Type      ActionableCommandType     `json:"type"`
	Reference string                    `json:"reference"`
	Options   *ActionableCommandOptions `json:"options,omitempty"`
}

// ActionableCommandOptions holds type-specific options for a command.
// Template and dashboard commands use Variable.
// URL commands use Modal, ModalWidth, and NoValueURLEncode.
type ActionableCommandOptions struct {
	Variable         string `json:"variable,omitempty"`
	Modal            bool   `json:"modal,omitempty"`
	ModalWidth       string `json:"modalWidth,omitempty"`
	NoValueURLEncode bool   `json:"noValueUrlEncode,omitempty"`
}

type ActionableListResponse struct {
	BaseListResponse
	Results []Actionable `json:"results"`
}
