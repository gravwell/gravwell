/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"github.com/gravwell/gravwell/v4/client/types"
)

// ListPlaybooks returns all playbooks accessible to the current user.
func (c *Client) ListPlaybooks(opts *types.QueryOptions) (ret types.PlaybookListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	return c.post[types.QueryOptions, types.PlaybookListResponse](PLAYBOOKS_LIST_URL, opts)
}

// ListAllPlaybooks (admin-only) returns all playbooks on the system.
func (c *Client) ListAllPlaybooks(opts *types.QueryOptions) (ret types.PlaybookListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	return c.post[types.QueryOptions, types.PlaybookListResponse](PLAYBOOKS_LIST_URL, opts)
}

// GetPlaybook returns a particular playbook.
func (c *Client) GetPlaybook(id string) (types.Playbook, error) {
	return c.GetPlaybookEx(id, GetOptions{})
}

// GetPlaybookEx returns a particular playbook, modified by opts.
func (c *Client) GetPlaybookEx(id string, opts GetOptions) (types.Playbook, error) {
	return c.get[types.Playbook](playbookUrl(id), opts.params()...)
}

// DeletePlaybook deletes a playbook by marking it deleted in the database.
func (c *Client) DeletePlaybook(id string) error {
	return c.delete(playbookUrl(id), false)
}

// PurgePlaybook deletes a playbook entirely, removing it from the database.
func (c *Client) PurgePlaybook(id string) error {
	return c.delete(playbookUrl(id), true)
}

// CreatePlaybook creates a new playbook, returning the newly-created playbook.
func (c *Client) CreatePlaybook(pb types.Playbook) (result types.Playbook, err error) {
	return c.post[types.Playbook, types.Playbook](PLAYBOOKS_URL, &pb)
}

// UpdatePlaybook modifies an existing playbook and returns the complete, updated struct.
func (c *Client) UpdatePlaybook(ID string, p types.PlaybookPatch) (updated types.Playbook, err error) {
	if ID == "" {
		return types.Playbook{}, ErrEmptyID
	}
	return c.patch[types.PlaybookPatch, types.Playbook](playbookUrl(ID), p)
}

// CleanupPlaybooks (admin-only) purges all deleted playbooks for all users.
func (c *Client) CleanupPlaybooks() error {
	return c.delete(PLAYBOOKS_URL, false)
}
