/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import "encoding/json"

// Playbook configuration, including ownership, description, etc., as well as
// the playbook content.
type Playbook struct {
	CommonFields
	Body string
	// Cover and Banner are IDs of files
	Cover         string
	Banner        string
	AuthorName    string
	AuthorEmail   string
	AuthorCompany string
	AuthorURL     string
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (p Playbook) MarshalJSON() ([]byte, error) {
	type dummyPlaybook Playbook
	p.CommonFields = p.CommonFields.MakeNilSlices()
	return json.Marshal(dummyPlaybook(p))
}

type PlaybookListResponse struct {
	BaseListResponse
	Results []Playbook
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (p PlaybookListResponse) MarshalJSON() ([]byte, error) {
	type dummyPlaybookListResponse PlaybookListResponse
	p.Results = nonNilSlice(p.Results)
	p.BaseListResponse = p.BaseListResponse.MakeNilSlices()
	return json.Marshal(dummyPlaybookListResponse(p))
}
