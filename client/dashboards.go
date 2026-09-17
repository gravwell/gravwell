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
	return c.post[types.QueryOptions, types.DashboardListResponse](DASHBOARDS_LIST_URL, opts)
}

// ListAllDashboards (admin-only) returns all dashboards on the system.
func (c *Client) ListAllDashboards(opts *types.QueryOptions) (ret types.DashboardListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true
	return c.post[types.QueryOptions, types.DashboardListResponse](DASHBOARDS_LIST_URL, opts)
}

// GetDashboard returns a particular dashboard.
func (c *Client) GetDashboard(id string) (types.Dashboard, error) {
	return c.GetDashboardEx(id, GetOptions{})
}

// GetDashboardEx returns a particular dashboard, modified by opts.
func (c *Client) GetDashboardEx(id string, opts GetOptions) (types.Dashboard, error) {
	return c.get[types.Dashboard](dashboardIdUrl(id), opts.params()...)
}

// DeleteDashboard deletes a dashboard by marking it deleted in the database.
func (c *Client) DeleteDashboard(id string) error {
	return c.delete(dashboardIdUrl(id), false)
}

// PurgeDashboard deletes a dashboard entirely, removing it from the database.
func (c *Client) PurgeDashboard(id string) error {
	return c.delete(dashboardIdUrl(id), true)
}

// CreateDashboard creates a new dashboard, returning the newly-created dashboard.
func (c *Client) CreateDashboard(d types.Dashboard) (result types.Dashboard, err error) {
	return c.post[types.Dashboard, types.Dashboard](DASHBOARDS_URL, &d)
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
	return c.delete(DASHBOARDS_URL, false)
}
