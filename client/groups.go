/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"github.com/gravwell/gravwell/v4/client/types"
)

// ListGroups returns a list of groups on the system. If CBAC is
// enabled, regular users must possess the ListGroups capability or
// the function will return an error.
func (c *Client) ListGroups(opts *types.QueryOptions) (ret types.GroupListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(GROUP_LIST_URL, opts, &ret)
	return
}

// GetGroupMap returns a map of GID to group name for every group on
// the system. This calls ListGroups under the hood, so the user must
// have the ListGroups capability enabled.
func (c *Client) GetGroupMap() (map[int32]string, error) {
	groups, err := c.ListGroups(nil)
	if err != nil {
		return nil, err
	}
	m := make(map[int32]string, len(groups.Results))
	for _, g := range groups.Results {
		m[g.ID] = g.Name
	}
	return m, nil
}

// GetGroup returns information about the specified group.
func (c *Client) GetGroup(id int32) (types.GroupWithCBAC, error) {
	var gp types.GroupWithCBAC
	if err := c.getStaticURL(groupIdUrl(id), &gp); err != nil {
		return gp, err
	}
	return gp, nil
}

// GetGroupEx returns a particular group. If the QueryOptions arg is
// not nil, applicable parameters (currently only IncludeDeleted) will
// be applied to the query.
func (c *Client) GetGroupEx(id int32, opts *types.QueryOptions) (types.GroupWithCBAC, error) {
	var gp types.GroupWithCBAC
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err := c.getStaticURL(groupIdUrl(id), &gp, ezParam("include_deleted", opts.IncludeDeleted))
	return gp, err
}

// DeleteGroup deletes a group by marking it deleted in the database.
func (c *Client) DeleteGroup(gid int32) error {
	return c.deleteStaticURL(groupIdUrl(gid), nil)
}

// CreateGroup creates a new group, returning the newly-created group.
func (c *Client) CreateGroup(m types.Group) (result types.Group, err error) {
	err = c.postStaticURL(GROUP_URL, m, &result)
	return
}

// UpdateGroup modifies an existing group.
func (c *Client) UpdateGroup(gdet types.Group) error {
	return c.putStaticURL(groupIdUrl(gdet.ID), gdet)
}

// CleanupGroups (admin-only) purges all deleted groups.
func (c *Client) CleanupGroups() error {
	return c.deleteStaticURL(GROUP_URL, nil)
}

// LookupGroup looks up a Group object given a group name.  If the
// group name is not found, ErrNotFound is returned. This calls
// ListGroups under the hood, so the user must have the ListGroups
// capability enabled.
func (c *Client) LookupGroup(groupname string) (gd types.Group, err error) {
	var lst types.GroupListResponse
	if lst, err = c.ListGroups(nil); err != nil {
		return
	}
	for _, l := range lst.Results {
		if l.Name == groupname {
			gd = l
			return
		}
	}

	err = ErrNotFound
	return
}
