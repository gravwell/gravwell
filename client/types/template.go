/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

// Template is a stored Gravwell query template with variables.
type Template struct {
	CommonFields

	Query     string
	Variables []TemplateVariable
}

type TemplateVariable struct {
	Name         string
	Label        string
	Description  string
	Required     bool
	DefaultValue string
	PreviewValue string
}

// TemplatePatch is the type used to request an update to an existing Template.
type TemplatePatch struct {
	CommonFieldsPatch
	Query     Optional[string]             `json:",omitzero"`
	Variables Optional[[]TemplateVariable] `json:",omitzero"`
}

// ToPatch converts t into a TemplatePatch with every field set.
func (t Template) ToPatch() TemplatePatch {
	return TemplatePatch{
		CommonFieldsPatch: t.CommonFields.ToPatch(),
		Query:             NewOptional(t.Query),
		Variables:         NewOptional(t.Variables),
	}
}

type TemplateListResponse struct {
	BaseListResponse
	Results []Template
}
