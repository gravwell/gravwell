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

type PlaybookListResponse struct {
	BaseListResponse
	Results []Playbook `json:"results"`
}
