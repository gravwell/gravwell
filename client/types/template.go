/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import "encoding/json"

// Template is a stored Gravwell query template with variables.
type Template struct {
	CommonFields

	Query     string
	Variables []TemplateVariable
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (t Template) MarshalJSON() ([]byte, error) {
	type dummyTemplate Template
	t.CommonFields = t.CommonFields.MakeNilSlices()
	t.Variables = nonNilSlice(t.Variables)
	return json.Marshal(dummyTemplate(t))
}

type TemplateVariable struct {
	Name         string
	Label        string
	Description  string
	Required     bool
	DefaultValue string
	PreviewValue string
}

type TemplateListResponse struct {
	BaseListResponse
	Results []Template
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (t TemplateListResponse) MarshalJSON() ([]byte, error) {
	type dummyTemplateListResponse TemplateListResponse
	t.Results = nonNilSlice(t.Results)
	t.BaseListResponse = t.BaseListResponse.MakeNilSlices()
	return json.Marshal(dummyTemplateListResponse(t))
}
