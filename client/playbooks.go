/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"net/http"

	"github.com/gravwell/gravwell/v4/client/types"
)

// ListPlaybooks returns all playbooks accessible to the current user.
func (c *Client) ListPlaybooks(opts *types.QueryOptions) (ret types.PlaybookListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(PLAYBOOKS_LIST_URL, opts, &ret)
	return
}

// ListAllPlaybooks (admin-only) returns all playbooks on the system.
func (c *Client) ListAllPlaybooks(opts *types.QueryOptions) (ret types.PlaybookListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	err = c.postStaticURL(PLAYBOOKS_LIST_URL, opts, &ret)
	return
}

// GetPlaybook returns a particular playbook.
func (c *Client) GetPlaybook(id string) (types.Playbook, error) {
	var pb types.Playbook
	err := c.getStaticURL(playbookUrl(id), &pb)
	return pb, err
}

// GetPlaybookEx returns a particular playbook. If the QueryOptions arg is
// not nil, applicable parameters (currently only IncludeDeleted) will
// be applied to the query.
func (c *Client) GetPlaybookEx(id string, opts *types.QueryOptions) (types.Playbook, error) {
	var pb types.Playbook
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err := c.getStaticURL(playbookUrl(id), &pb, ezParam("include_deleted", opts.IncludeDeleted))
	return pb, err
}

// DeletePlaybook deletes a playbook by marking it deleted in the database.
func (c *Client) DeletePlaybook(id string) error {
	return c.deleteStaticURL(playbookUrl(id), nil)
}

// PurgePlaybook deletes a playbook entirely, removing it from the database.
func (c *Client) PurgePlaybook(id string) error {
	return c.deleteStaticURL(playbookUrl(id), nil, ezParam("purge", "true"))
}

// CreatePlaybook creates a new playbook, returning the newly-created playbook.
func (c *Client) CreatePlaybook(pb types.Playbook) (result types.Playbook, err error) {
	err = c.postStaticURL(PLAYBOOKS_URL, pb, &result)
	return
}

// UpdatePlaybook modifies an existing playbook.
func (c *Client) UpdatePlaybook(pb types.Playbook) (result types.Playbook, err error) {
	err = c.methodStaticPushURL(http.MethodPut, playbookUrl(pb.ID), pb, &result, nil, nil)
	return
}

// CleanupPlaybooks (admin-only) purges all deleted playbooks for all users.
func (c *Client) CleanupPlaybooks() error {
	return c.deleteStaticURL(PLAYBOOKS_URL, nil)
}
