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
	return c.post[types.QueryOptions, types.GroupListResponse](GROUP_LIST_URL, opts)
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
	return c.GetGroupEx(id, GetOptions{})
}

// GetGroupEx returns a particular group, modified by opts.
func (c *Client) GetGroupEx(id int32, opts GetOptions) (types.GroupWithCBAC, error) {
	return c.get[types.GroupWithCBAC](groupIdUrl(id), opts.params()...)
}

// DeleteGroup deletes a group by marking it deleted in the database.
func (c *Client) DeleteGroup(gid int32) error {
	return c.delete(groupIdUrl(gid), false)
}

// CreateGroup creates a new group, returning the newly-created group.
func (c *Client) CreateGroup(m types.Group) (result types.Group, err error) {
	return c.post[types.Group, types.Group](GROUP_URL, &m)
}

// UpdateGroup modifies an existing group and returns the complete, updated struct.
func (c *Client) UpdateGroup(ID int32, p types.GroupPatch) (updated types.Group, err error) {
	if ID == 0 {
		return types.Group{}, ErrEmptyID
	}
	return c.patch[types.GroupPatch, types.Group](groupIdUrl(ID), p)
}

// CleanupGroups (admin-only) purges all deleted groups.
func (c *Client) CleanupGroups() error {
	return c.delete(groupUrl(), false)
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
