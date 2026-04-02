/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
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

// ListUserPreferences returns all user preferences accessible to the current user.
func (c *Client) ListUserPreferences(opts *types.QueryOptions) (ret types.UserPreferenceResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(USER_PREFERENCES_LIST_URL, opts, &ret)
	return
}

// ListAllUserPreferences (admin-only) returns all user preferences on the system.
func (c *Client) ListAllUserPreferences(opts *types.QueryOptions) (ret types.UserPreferenceResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	err = c.postStaticURL(USER_PREFERENCES_LIST_URL, opts, &ret)
	return
}

// GetUserPreference returns a particular user preference.
func (c *Client) GetUserPreference(id string) (types.UserPreference, error) {
	var pref types.UserPreference
	err := c.getStaticURL(userPreferenceUrl(id), &pref)
	return pref, err
}

// GetUserPreferenceEx returns a particular user preference. If the QueryOptions arg is
// not nil, applicable parameters (currently only IncludeDeleted) will
// be applied to the query.
func (c *Client) GetUserPreferenceEx(id string, opts *types.QueryOptions) (types.UserPreference, error) {
	var pref types.UserPreference
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err := c.getStaticURL(userPreferenceUrl(id), &pref, ezParam("include_deleted", opts.IncludeDeleted))
	return pref, err
}

// GetUserPreferenceByName returns the user preference with the given name owned by the
// currently logged-in user.
func (c *Client) GetUserPreferenceByName(name string) (types.UserPreference, error) {
	if c.userDetails.ID == 0 {
		return types.UserPreference{}, ErrNotSynced
	}
	opts := types.QueryOptions{
		OwnerID: c.userDetails.ID,
		Filters: []types.Filter{
			{Key: "Name", Operation: "=", Values: []any{name}},
		},
	}
	resp, err := c.ListUserPreferences(&opts)
	if err != nil {
		return types.UserPreference{}, err
	}
	if len(resp.Results) == 0 {
		return types.UserPreference{}, ErrNotFound
	}
	return resp.Results[0], nil
}

// DeleteUserPreference deletes a user preference by marking it deleted in the database.
func (c *Client) DeleteUserPreference(id string) error {
	return c.deleteStaticURL(userPreferenceUrl(id), nil)
}

// PurgeUserPreference deletes a user preference entirely, removing it from the database.
func (c *Client) PurgeUserPreference(id string) error {
	return c.deleteStaticURL(userPreferenceUrl(id), nil, ezParam("purge", "true"))
}

// CreateUserPreference creates a new user preference, returning the newly-created user preference.
func (c *Client) CreateUserPreference(p types.UserPreference) (result types.UserPreference, err error) {
	err = c.postStaticURL(USER_PREFERENCES_URL, p, &result)
	return
}

// UpdateUserPreference modifies an existing user preference.
func (c *Client) UpdateUserPreference(p types.UserPreference) (result types.UserPreference, err error) {
	err = c.methodStaticPushURL(http.MethodPut, userPreferenceUrl(p.ID), p, &result, nil, nil)
	return
}

// CleanupUserPreferences (admin-only) purges all deleted user preferences for all users.
func (c *Client) CleanupUserPreferences() error {
	return c.deleteStaticURL(USER_PREFERENCES_URL, nil)
}

// GetGuiPreferences is a convenience function: it returns the Data
// field of the preferences object named `prefs` belonging to the
// specified user, loading it into the specified object.
func (c *Client) GetGuiPreferences(uid int32, obj interface{}) error {
	return c.getStaticURL(preferencesUrl(uid), obj)
}

// DeleteGuiPreferences clears the Data field of the preferences
// object named `prefs` belonging to the specified user. It does *not*
// delete the underlying asset, though.
func (c *Client) DeleteGuiPreferences(id int32) error {
	return c.deleteStaticURL(preferencesUrl(id), nil)
}

// PutGuiPreferences updates the Data field of the preferences object
// named `prefs` belonging to the specified user.
func (c *Client) PutGuiPreferences(id int32, obj interface{}) error {
	return c.putStaticURL(preferencesUrl(id), obj)
}
