/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"fmt"

	"github.com/gravwell/gravwell/v3/client/types"
)

// CapabilityList returns a complete list of capabilities.
func (c *Client) CapabilityList() (cl []types.CapabilityDesc, err error) {
	err = c.getStaticURL(CAPABILITY_LIST_URL, &cl)
	return
}

// CapabilityTemplateList returns a list of ABAC templates defined on the system.
func (c *Client) CapabilityTemplateList() (cl []types.CapabilityTemplate, err error) {
	err = c.getStaticURL(CAPABILITY_TEMPLATE_LIST_URL, &cl)
	return
}

// CurrentUserCapabilities returns the list of capabilities enabled for the current user.
func (c *Client) CurrentUserCapabilities() (set []types.CapabilityDesc, err error) {
	err = c.getStaticURL(CAPABILITY_CURRENT_USER_LIST_URL, &set)
	return
}

// GetUserCapabilities (admin-only) returns the list of capabilities enabled
// for the specified user.
func (c *Client) GetUserCapabilities(uid int32) (cs types.CapabilityState, err error) {
	err = c.getStaticURL(fmt.Sprintf(CAPABILITY_USER_URL, uid), &cs)
	return
}

// SetUserCaapbilities (admin-only) sets a user's capabilities to the provided list.
func (c *Client) SetUserCapabilities(uid int32, cs types.CapabilityState) (err error) {
	err = c.putStaticURL(fmt.Sprintf(CAPABILITY_USER_URL, uid), &cs)
	return
}

// GetGroupCapabilities (admin-only) returns the list of capabilities enabled
// for a given group.
func (c *Client) GetGroupCapabilities(gid int32) (cs types.CapabilityState, err error) {
	err = c.getStaticURL(fmt.Sprintf(CAPABILITY_GROUP_URL, gid), &cs)
	return
}

// SetGroupCapabilities (admin-only) sets the capability list for a group.
func (c *Client) SetGroupCapabilities(gid int32, cs types.CapabilityState) (err error) {
	err = c.putStaticURL(fmt.Sprintf(CAPABILITY_GROUP_URL, gid), &cs)
	return
}

// GetUserTagAccess (admin-only) returns the tag access restrictions for the specified user.
func (c *Client) GetUserTagAccess(uid int32) (ta types.TagAccess, err error) {
	err = c.getStaticURL(fmt.Sprintf(USER_TAG_ACCESS_URL, uid), &ta)
	return
}

// GetGroupTagAccess (admin-only) returns the tag access restrictions for the specified group.
func (c *Client) GetGroupTagAccess(gid int32) (ta types.TagAccess, err error) {
	err = c.getStaticURL(fmt.Sprintf(GROUP_TAG_ACCESS_URL, gid), &ta)
	return
}

// SetUserTagAccess (admin-only) sets the tag access rules for a user.
func (c *Client) SetUserTagAccess(uid int32, ta types.TagAccess) (err error) {
	err = c.putStaticURL(fmt.Sprintf(USER_TAG_ACCESS_URL, uid), ta)
	return
}

// SetGroupTagAccess (admin-only) sets the tag access rules for a group.
func (c *Client) SetGroupTagAccess(gid int32, ta types.TagAccess) (err error) {
	err = c.putStaticURL(fmt.Sprintf(GROUP_TAG_ACCESS_URL, gid), ta)
	return
}

// ResetDefaultABAC (admin-only) clear the default ABAC rule
func (c *Client) ResetDefaultABAC() (err error) {
	err = c.deleteStaticURL(ABAC_DEFAULT_URL, nil)
	return
}

// DefaultABACTagAccess (admin-only) pulls the tag access state for the default ABAC rule
func (c *Client) DefaultABACTagAccess() (ta types.TagAccess, err error) {
	err = c.getStaticURL(ABAC_DEFAULT_TAGS_URL, &ta)
	return
}

// DefaultABACCapabilities (admin-only) pulls the capability state for the default ABAC rule
func (c *Client) DefaultABACCapabilities() (cs types.CapabilityState, err error) {
	err = c.getStaticURL(ABAC_DEFAULT_CAPABILITIES_URL, &cs)
	return
}

// SetDefaultABACTagAccess (admin-only) sets the tag access state for the default ABAC rule
func (c *Client) SetDefaultABACTagAccess(ta types.TagAccess) (err error) {
	err = c.postStaticURL(ABAC_DEFAULT_TAGS_URL, ta, nil)
	return
}

// SetDefaultABACCapabilities (admin-only) sets the capability state for the default ABAC rule
func (c *Client) SetDefaultABACCapabilities(cs types.CapabilityState) (err error) {
	err = c.postStaticURL(ABAC_DEFAULT_CAPABILITIES_URL, cs, nil)
	return
}
