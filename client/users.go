/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"errors"
	"net/http"

	"github.com/gravwell/gravwell/v4/client/types"
)

// ListUsers returns a list of users accessible to the current user.
func (c *Client) ListUsers(opts *types.QueryOptions) (ret types.UserListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(USERS_LIST_URL, opts, &ret)
	return
}

// ListAllUsers (admin-only) returns all users on the system.
func (c *Client) ListAllUsers(opts *types.QueryOptions) (ret types.UserListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	err = c.postStaticURL(USERS_LIST_URL, opts, &ret)
	return
}

// GetUserMap returns a map of UID to username for every user on the system.
func (c *Client) GetUserMap() (map[int32]string, error) {
	users, err := c.ListAllUsers(nil)
	if err != nil {
		return nil, err
	}
	m := make(map[int32]string, len(users.Results))
	for _, u := range users.Results {
		m[u.ID] = u.Username
	}
	return m, nil
}

// GetUser returns a particular user.
func (c *Client) GetUser(id int32) (types.User, error) {
	var user types.User
	err := c.getStaticURL(usersInfoUrl(id), &user)
	return user, err
}

// GetUserEx returns a particular user. If the QueryOptions arg is
// not nil, applicable parameters (currently only IncludeDeleted) will
// be applied to the query.
func (c *Client) GetUserEx(id int32, opts *types.QueryOptions) (types.User, error) {
	var user types.User
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err := c.getStaticURL(usersInfoUrl(id), &user, ezParam("include_deleted", opts.IncludeDeleted))
	return user, err
}

// DeleteUser deletes a user by marking it deleted in the database.
func (c *Client) DeleteUser(id int32) error {
	return c.deleteStaticURL(usersInfoUrl(id), nil)
}

// PurgeUser is implemented in admin.go and also deletes the user's assets.

// CreateUser creates a new user, returning the newly-created user.
func (c *Client) CreateUser(m types.User) (result types.User, err error) {
	err = c.postStaticURL(USERS_URL, m, &result)
	return
}

// UpdateUser (admin-only) modifies an existing user.
func (c *Client) UpdateUser(m types.User) error {
	return c.putStaticURL(usersInfoUrl(m.ID), m)
}

// UpdateUserInfo changes basic information about the specified user.
// Admins can set any user's info, but regular users can only set their own.
func (c *Client) UpdateUserInfo(id int32, user, name, email string) error {
	me, err := c.MyInfo()
	if err != nil {
		return err
	}
	udet := me
	if id != me.ID {
		if !me.Admin {
			return errors.New("Only admins can change another user's info")
		} else {
			if err := c.methodStaticURL(http.MethodGet, usersInfoUrl(id), &udet); err != nil {
				return err
			}
		}
	}

	var gids []int32
	for _, g := range udet.DefaultSearchGroups {
		gids = append(gids, g.ID)
	}
	req := types.UpdateUser{
		Username:            user,
		Name:                name,
		Email:               email,
		Admin:               udet.Admin,
		Locked:              udet.Locked,
		DefaultSearchGroups: gids,
	}
	return c.methodStaticPushURL(http.MethodPut, usersInfoUrl(id), req, nil, nil, nil)
}

// CleanupUsers (admin-only) purges all deleted users for all users.
func (c *Client) CleanupUsers() error {
	return c.deleteStaticURL(USERS_URL, nil)
}
