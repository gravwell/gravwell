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

	"github.com/gravwell/gravwell/v4/client/types"
)

// ListUsers returns a list of users. If CBAC is enabled, regular
// users must possess the ListUsers capability or the function will
// return an error.
func (c *Client) ListUsers(opts *types.QueryOptions) (ret types.UserListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	return c.post[types.QueryOptions, types.UserListResponse](USERS_LIST_URL, opts)
}

// GetUserMap returns a map of UID to username for every user on the system. This calls ListUsers under the hood, so the user must have the ListUsers capability enabled.
func (c *Client) GetUserMap() (map[int32]string, error) {
	users, err := c.ListUsers(nil)
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
func (c *Client) GetUser(id int32) (types.UserWithCBAC, error) {
	return c.GetUserEx(id, GetOptions{})
}

// GetUserEx returns a particular user, modified by opts.
func (c *Client) GetUserEx(id int32, opts GetOptions) (types.UserWithCBAC, error) {
	return c.get[types.UserWithCBAC](usersInfoUrl(id), opts.params()...)
}

// DeleteUser deletes a user by marking it deleted in the database.
func (c *Client) DeleteUser(id int32) error {
	return c.delete(usersInfoUrl(id), false)
}

// PurgeUser is implemented in admin.go and also deletes the user's assets.

// CreateUser creates a new user, returning the newly-created
// user. Note that unlike most Create* types, this takes a special
// type which includes the password and leaves out other fields.
func (c *Client) CreateUser(m types.AddUser) (result types.User, err error) {
	return c.post[types.AddUser, types.User](USERS_URL, &m)
}

// UpdateUser (admin-only) modifies an existing user and returns the complete, updated struct.
func (c *Client) UpdateUser(ID int32, p types.UserPatch) (updated types.User, err error) {
	if ID == 0 {
		return updated, ErrEmptyID
	}
	return c.patch[types.UserPatch, types.User](usersInfoUrl(ID), p)
}

// UpdateUserInfo changes basic information about the specified user.
// Admins can set any user's info, but regular users can only set their own.
func (c *Client) UpdateUserInfo(id int32, user, name, email string) error {
	me, err := c.MyInfo()
	if err != nil {
		return err
	}
	if id != me.ID && !me.Admin {
		return errors.New("Only admins can change another user's info")
	}

	p := types.UserPatch{
		Username: types.NewOptional(user),
		Name:     types.NewOptional(name),
		Email:    types.NewOptional(email),
	}
	_, err = c.UpdateUser(id, p)
	return err
}

// CleanupUsers (admin-only) purges all deleted users for all users.
func (c *Client) CleanupUsers() error {
	return c.delete(USERS_URL, false)
}

// LookupUser looks up a User object given a username.  If the
// username is not found, ErrNotFound is returned.  This calls
// ListUsers under the hood, so the user must have the ListUsers
// capability enabled.
func (c *Client) LookupUser(username string) (ud types.User, err error) {
	var lst types.UserListResponse
	if lst, err = c.ListUsers(nil); err != nil {
		return
	}
	for _, l := range lst.Results {
		if l.Username == username {
			ud = l
			return
		}
	}

	err = ErrNotFound
	return
}
