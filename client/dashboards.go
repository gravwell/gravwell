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

// ListDashboards returns all dashboards accessible to the current user.
func (c *Client) ListDashboards(opts *types.QueryOptions) (ret types.DashboardListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(DASHBOARDS_LIST_URL, opts, &ret)
	return
}

// ListAllDashboards (admin-only) returns all dashboards on the system.
func (c *Client) ListAllDashboards(opts *types.QueryOptions) (ret types.DashboardListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true
	err = c.postStaticURL(DASHBOARDS_LIST_URL, opts, &ret)
	return
}

// GetDashboard returns a particular dashboard.
func (c *Client) GetDashboard(id string) (types.Dashboard, error) {
	var dashboard types.Dashboard
	err := c.getStaticURL(dashboardIdUrl(id), &dashboard)
	return dashboard, err
}

// GetDashboardEx returns a particular dashboard. If the QueryOptions arg is
// not nil, applicable parameters (currently only IncludeDeleted) will
// be applied to the query.
func (c *Client) GetDashboardEx(id string, opts *types.QueryOptions) (types.Dashboard, error) {
	var dashboard types.Dashboard
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err := c.getStaticURL(dashboardIdUrl(id), &dashboard, ezParam("include_deleted", opts.IncludeDeleted))
	return dashboard, err
}

// DeleteDashboard deletes a dashboard by marking it deleted in the database.
func (c *Client) DeleteDashboard(id string) error {
	return c.deleteStaticURL(dashboardIdUrl(id), nil)
}

// PurgeDashboard deletes a dashboard entirely, removing it from the database.
func (c *Client) PurgeDashboard(id string) error {
	return c.deleteStaticURL(dashboardIdUrl(id), nil, ezParam("purge", "true"))
}

// CreateDashboard creates a new dashboard, returning the newly-created dashboard.
func (c *Client) CreateDashboard(d types.Dashboard) (result types.Dashboard, err error) {
	err = c.postStaticURL(DASHBOARDS_URL, d, &result)
	return
}

// UpdateDashboard modifies an existing dashboard and returns the complete, updated struct.
func (c *Client) UpdateDashboard(ID string, p types.DashboardPatch) (updated types.Dashboard, err error) {
	if ID == "" {
		return types.Dashboard{}, ErrEmptyID
	}
	return c.patch[types.DashboardPatch, types.Dashboard](dashboardIdUrl(ID), p)
}

// CleanupDashboards (admin-only) purges all deleted dashboards for all users.
func (c *Client) CleanupDashboards() error {
	return c.deleteStaticURL(DASHBOARDS_URL, nil)
}
