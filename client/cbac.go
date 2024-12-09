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

	"github.com/gravwell/gravwell/v4/client/types"
)

// CapabilityList returns a complete list of capabilities.
func (c *Client) CapabilityList() (cl []types.CapabilityDesc, err error) {
	err = c.getStaticURL(CAPABILITY_LIST_URL, &cl)
	return
}

// CapabilityTemplateList returns a list of CBAC templates defined on the system.
func (c *Client) CapabilityTemplateList() (cl []types.CapabilityTemplate, err error) {
	err = c.getStaticURL(CAPABILITY_TEMPLATE_LIST_URL, &cl)
	return
}

// CurrentUserCapabilities returns the list of capabilities enabled for the current user.
func (c *Client) CurrentUserCapabilities() (set []types.CapabilityDesc, err error) {
	err = c.getStaticURL(CAPABILITY_CURRENT_USER_LIST_URL, &set)
	return
}

// CurrentUserCapabilityExplanations returns the list of capabilities, marked up to explain
// whether or not a user has the capability and why.
func (c *Client) CurrentUserCapabilityExplanations() (set []types.CapabilityExplanation, err error) {
	err = c.getStaticURL(CAPABILITY_CURRENT_USER_WHY_URL, &set)
	return
}

// HasCapability checks if the client contains a given capability, if the capability list is not yet populated
func (c *Client) HasCapability(cp types.Capability) bool {
	if c.capabilities == nil {
		var err error
		if c.capabilities, err = c.CurrentUserCapabilities(); err != nil {
			return false
		}
	}
	for _, v := range c.capabilities {
		if v.Cap == cp {
			return true
		}
	}
	return false
}

// GetUserCapabilities (admin-only) returns the list of capabilities enabled
// for the specified user.
func (c *Client) GetUserCapabilities(uid int32) (cs types.CapabilityState, err error) {
	err = c.getStaticURL(fmt.Sprintf(CAPABILITY_USER_URL, uid), &cs)
	return
}

// GetUserCapabilityExplanations (admin-only) returns the list of capabilities enabled
// for the specified user & why
func (c *Client) GetUserCapabilityExplanations(uid int32) (cs []types.CapabilityExplanation, err error) {
	err = c.getStaticURL(fmt.Sprintf(CAPABILITY_USER_WHY_URL, uid), &cs)
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
