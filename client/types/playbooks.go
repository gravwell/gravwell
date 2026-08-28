/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

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

// PlaybookPatch is the type used to request an update to an existing Playbook.
type PlaybookPatch struct {
	CommonFieldsPatch
	AuthorCompany Optional[string] `json:",omitzero"`
	AuthorEmail   Optional[string] `json:",omitzero"`
	AuthorName    Optional[string] `json:",omitzero"`
	AuthorURL     Optional[string] `json:",omitzero"`
	Banner        Optional[string] `json:",omitzero"`
	Body          Optional[string] `json:",omitzero"`
	Cover         Optional[string] `json:",omitzero"`
}

// ToPatch converts pb into a PlaybookPatch with every field set.
func (pb Playbook) ToPatch() PlaybookPatch {
	return PlaybookPatch{
		CommonFieldsPatch: pb.CommonFields.ToPatch(),
		AuthorCompany:     NewOptional(pb.AuthorCompany),
		AuthorEmail:       NewOptional(pb.AuthorEmail),
		AuthorName:        NewOptional(pb.AuthorName),
		AuthorURL:         NewOptional(pb.AuthorURL),
		Banner:            NewOptional(pb.Banner),
		Body:              NewOptional(pb.Body),
		Cover:             NewOptional(pb.Cover),
	}
}

type PlaybookListResponse struct {
	BaseListResponse
	Results []Playbook
}
