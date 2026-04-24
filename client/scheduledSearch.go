/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"github.com/gravwell/gravwell/v4/client/types"
)

// ListScheduledSearches returns scheduled searches the user has access to.
func (c *Client) ListScheduledSearches(opts *types.QueryOptions) (searches types.ScheduledSearchListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	if err = c.postStaticURL(SCHEDULED_SEARCH_LIST_URL, opts, &searches); err != nil {
		return
	}
	return
}

// ListAllScheduledSearches returns all scheduled searches on the system (for admins).
func (c *Client) ListAllScheduledSearches(opts *types.QueryOptions) (searches types.ScheduledSearchListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	if err = c.postStaticURL(SCHEDULED_SEARCH_LIST_URL, opts, &searches); err != nil {
		return
	}
	return
}

// GetScheduledSearch returns the scheduled search with the given ID.
func (c *Client) GetScheduledSearch(id string) (types.ScheduledSearch, error) {
	var search types.ScheduledSearch
	err := c.getStaticURL(scheduledSearchIdUrl(id), &search)
	return search, err
}

// GetScheduledSearchEx returns a particular scheduled search. If the QueryOptions arg is
// not nil, applicable parameters (currently only IncludeDeleted) will
// be applied to the query.
func (c *Client) GetScheduledSearchEx(id string, opts *types.QueryOptions) (types.ScheduledSearch, error) {
	var search types.ScheduledSearch
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err := c.getStaticURL(scheduledSearchIdUrl(id), &search, ezParam("include_deleted", opts.IncludeDeleted))
	return search, err
}

// DeleteScheduledSearch removes the specified scheduled search.
func (c *Client) DeleteScheduledSearch(id string) error {
	return c.deleteStaticURL(scheduledSearchIdUrl(id), nil)
}

// PurgeScheduledSearch permanently removes the specified scheduled search.
func (c *Client) PurgeScheduledSearch(id string) error {
	return c.deleteStaticURL(scheduledSearchIdUrl(id), nil, ezParam("purge", "true"))
}

// CreateScheduledSearch makes a new scheduled search.
func (c *Client) CreateScheduledSearch(spec types.ScheduledSearch) (result types.ScheduledSearch, err error) {
	err = c.postStaticURL(scheduledSearchUrl(), spec, &result)
	return
}

// UpdateScheduledSearch is used to modify an existing scheduled search.
func (c *Client) UpdateScheduledSearch(ss types.ScheduledSearch) error {
	return c.putStaticURL(scheduledSearchIdUrl(ss.ID), ss)
}

// UpdateScheduledSearchResults is used to update the scheduled search after it has been
// run. It only updates the PersistentMaps, LastRun, LastRunDuration, LastSearchIDs,
// and LastError fields
func (c *Client) UpdateScheduledSearchResults(ss types.ScheduledSearch) error {
	return c.putStaticURL(scheduledSearchResultsIdUrl(ss.ID), ss)
}

// ScheduledSearchCheckin (admin-only) informs the webserver that the search agent is active and passes along info about what it is currently doing. The server may send back new jobs, or jobs to cancel, in the response.
func (c *Client) ScheduledSearchCheckin(cfg types.SearchAgentCheckin) (types.SearchAgentCheckinResponse, error) {
	var resp types.SearchAgentCheckinResponse
	err := c.postStaticURL(scheduledSearchCheckinUrl(), cfg, &resp)
	return resp, err
}

// ReportScheduledSearchResults uploads a set of results for the scheduled search with the specified ID.
func (c *Client) ReportScheduledSearchResults(id string, results types.ScheduledSearchResults) error {
	return c.postStaticURL(scheduledSearchResultsIdUrl(id), results, nil)
}

// GetScheduledSearchResults retrieves the most recent results for the specified scheduled search
func (c *Client) GetScheduledSearchResults(id string) (results types.ScheduledSearchResults, err error) {
	err = c.getStaticURL(scheduledSearchResultsIdUrl(id), &results)
	return
}

// ClearScheduledSearchResults deletes all results for the specified scheduled search
func (c *Client) ClearScheduledSearchResults(id string) error {
	return c.deleteStaticURL(scheduledSearchResultsIdUrl(id), nil)
}

// DebugScheduledSearch requests an immediate debug run of the specified scheduled search.
func (c *Client) DebugScheduledSearch(id string, opts types.AutomationDebugRequest) error {
	return c.postStaticURL(scheduledSearchDebugIdUrl(id), opts, nil)
}

// CancelScheduledSearch cancels any active run of the specified scheduled search.
func (c *Client) CancelScheduledSearch(id string) error {
	return c.deleteStaticURL(scheduledSearchCancelIdUrl(id), nil)
}
