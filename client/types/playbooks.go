/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Playbook configuration, including ownership, description, etc., as well as
// the playbook content.
type Playbook struct {
	UUID        uuid.UUID
	GUID        uuid.UUID // global identifier, used to uniquely identify a playbook rather than as a key in the webstore. Sorry.
	UID         int32
	GIDs        []int32
	Global      bool
	Name        string
	Desc        string
	Body        []byte `json:",omitempty"`
	Metadata    []byte `json:",omitempty"`
	Labels      []string
	LastUpdated time.Time
	Author      AuthorInfo
	Synced      bool
}

type AuthorInfo struct {
	Name    string
	Email   string
	Company string
	URL     string
}

func (pb Playbook) JSONMetadata() (json.RawMessage, error) {
	b, err := json.Marshal(&struct {
		UUID        string
		Name        string
		Description string
	}{
		UUID:        pb.GUID.String(),
		Name:        pb.Name,
		Description: pb.Desc,
	})
	return json.RawMessage(b), err
}
