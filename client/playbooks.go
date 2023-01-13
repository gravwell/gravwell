/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"github.com/gravwell/gravwell/v3/client/types"

	"github.com/google/uuid"
)

// GetUserPlaybooks returns all playbooks accessible to the current user.
func (c *Client) GetUserPlaybooks() ([]types.Playbook, error) {
	var pbs []types.Playbook
	if err := c.getStaticURL(PLAYBOOKS_URL, &pbs); err != nil {
		return nil, err
	}
	return pbs, nil
}

// GetPlaybook fetches the playbook with the specified UUID or GUID.
func (c *Client) GetPlaybook(id uuid.UUID) (types.Playbook, error) {
	var pb types.Playbook
	err := c.getStaticURL(playbookUrl(id), &pb)
	return pb, err
}

// DeletePlaybook deletes the playbook with the specified UUID or GUID.
func (c *Client) DeletePlaybook(id uuid.UUID) error {
	return c.deleteStaticURL(playbookUrl(id), nil)
}

// AddPlaybook creates a new playbook with the specified name, description, and body, returning
// the UUID of the new playbook. Note that the UUID and GUID fields will be automatically chosen,
// but the GUID field may be updated later.
func (c *Client) AddPlaybook(name, desc string, body []byte) (uuid.UUID, error) {
	m := types.Playbook{Name: name, Desc: desc, Body: body}
	var id uuid.UUID
	err := c.postStaticURL(PLAYBOOKS_URL, m, &id)
	return id, err
}

// UpdatePlaybook modifies an existing playbook. The UUID or GUID field of the parameter
// must match an existing playbook on the system that the user has access to.
func (c *Client) UpdatePlaybook(m types.Playbook) error {
	return c.putStaticURL(playbookUrl(m.UUID), m)
}

// GetAllPlaybooks (admin-only) returns all playbooks for all users.
// Non-administrators will receive the same list as returned by GetUserPlaybooks.
func (c *Client) GetAllPlaybooks() (pbs []types.Playbook, err error) {
	//check our status locally, server will kick it too, but no reason in even
	//making the request if we know it will fail
	if !c.userDetails.Admin {
		err = ErrNotAdmin
	} else {
		c.SetAdminMode()
		if err = c.getStaticURL(PLAYBOOKS_URL, &pbs); err != nil {
			pbs = nil
		}
		c.ClearAdminMode()
	}
	return
}
