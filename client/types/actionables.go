/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"github.com/google/uuid"
)

const (
	ACTIONABLE_COMMAND_QUERY       ActionableCommandType = "query"
	ACTIONABLE_COMMAND_TEMPLATE    ActionableCommandType = "template"
	ACTIONABLE_COMMAND_DASHBOARD   ActionableCommandType = "dashboard"
	ACTIONABLE_COMMAND_SAVED_QUERY ActionableCommandType = "savedQuery"
	ACTIONABLE_COMMAND_URL         ActionableCommandType = "url"
)

type ActionableCommandType string

// StrictPivot is a Pivot which actually decodes the Contents field,
// rather than treating it as a RawObject. This is for migration to
// the registry.
type StrictPivot struct {
	GUID        uuid.UUID
	Name        string
	Description string
	Contents    ActionableContent
	Labels      []string
	Disabled    bool
}

// ActionableContent defines the content of an actionable (pivot),
// including its menu label, triggers, and actions.
type ActionableContent struct {
	MenuLabel string              `json:"menuLabel"`
	Actions   []ActionableAction  `json:"actions"`
	Triggers  []ActionableTrigger `json:"triggers"`
}

// ActionableTrigger defines a pattern that activates an actionable.
type ActionableTrigger struct {
	Pattern   string `json:"pattern"`
	Hyperlink bool   `json:"hyperlink"`
}

// ActionableAction defines an action that can be performed when an actionable is triggered.
type ActionableAction struct {
	Name        string                `json:"name,omitempty"`
	Description string                `json:"description,omitempty"`
	Placeholder string                `json:"placeholder,omitempty"`
	Start       *ActionableTimeOption `json:"start,omitempty"`
	End         *ActionableTimeOption `json:"end,omitempty"`
	Command     ActionableCommand     `json:"command"`
}

// ActionableTimeOption describes time-range options for an action's start or end.
type ActionableTimeOption struct {
	Type        string `json:"type,omitempty"`
	Format      string `json:"format,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

type ActionableCommand struct {
	Type      ActionableCommandType     `json:"type"`
	Reference string                    `json:"reference"`
	Options   *ActionableCommandOptions `json:"options,omitempty"`
}

type ActionableCommandOptions struct {
	// For template or dashboard commands
	Variable string `json:"variable,omitempty"`

	// For URL commands
	Modal            string `json:"modal,omitempty"`
	ModalWidth       string `json:"modalWidth,omitempty"`
	NoValueURLEncode string `json:"noValueUrlEncode,omitempty"`
}
