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
func (c *Client) ListScheduledSearches(opts types.QueryOptions) (searches types.ScheduledSearchListResponse, err error) {
	return c.post[types.QueryOptions, types.ScheduledSearchListResponse](LIST_SCHEDULED_SEARCHES_URL, &opts)
}

// ListAllScheduledSearches returns all scheduled searches on the system (for admins).
func (c *Client) ListAllScheduledSearches(opts types.QueryOptions) (searches types.ScheduledSearchListResponse, err error) {
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	return c.post[types.QueryOptions, types.ScheduledSearchListResponse](LIST_SCHEDULED_SEARCHES_URL, &opts)
}

// GetScheduledSearch returns the scheduled search with the given ID.
func (c *Client) GetScheduledSearch(id string) (types.ScheduledSearch, error) {
	return c.GetScheduledSearchEx(id, GetOptions{})
}

// GetScheduledSearchEx returns a particular scheduled search, modified by opts.
func (c *Client) GetScheduledSearchEx(id string, opts GetOptions) (types.ScheduledSearch, error) {
	return c.get[types.ScheduledSearch](scheduledSearchesIdUrl(id), opts.params()...)
}

// DeleteScheduledSearch removes the specified scheduled search.
func (c *Client) DeleteScheduledSearch(id string) error {
	return c.delete(scheduledSearchesIdUrl(id), false)
}

// PurgeScheduledSearch permanently removes the specified scheduled search.
func (c *Client) PurgeScheduledSearch(id string) error {
	return c.delete(scheduledSearchesIdUrl(id), true)
}

// CreateScheduledSearch makes a new scheduled search.
func (c *Client) CreateScheduledSearch(spec types.ScheduledSearch) (result types.ScheduledSearch, err error) {
	return c.post[types.ScheduledSearch, types.ScheduledSearch](scheduledSearchesUrl(), &spec)
}

// UpdateScheduledSearch modifies an existing scheduled search and returns the complete, updated struct.
func (c *Client) UpdateScheduledSearch(ID string, p types.ScheduledSearchPatch) (updated types.ScheduledSearch, err error) {
	if ID == "" {
		return types.ScheduledSearch{}, ErrEmptyID
	}
	return c.patch[types.ScheduledSearchPatch, types.ScheduledSearch](scheduledSearchesIdUrl(ID), p)
}

// UpdateScheduledSearchResults is used to update the scheduled search after it has been
// run. It only updates the PersistentMaps, LastRun, LastRunDuration, LastSearchIDs,
// and LastError fields
func (c *Client) UpdateScheduledSearchResults(ss types.ScheduledSearch) error {
	return c.putStaticURL(scheduledSearchesIdResultsUrl(ss.ID), ss)
}

// ScheduledSearchCheckin (admin-only) informs the webserver that the search agent is active and passes along info about what it is currently doing. The server may send back new jobs, or jobs to cancel, in the response.
func (c *Client) ScheduledSearchCheckin(cfg types.SearchAgentCheckin) (types.SearchAgentCheckinResponse, error) {
	cfg.Cfg.Search_Agent_Auth = ""
	return c.post[types.SearchAgentCheckin, types.SearchAgentCheckinResponse](searchAgentCheckinUrl(), &cfg)
}

// GetSearchAgentStatus returns information about searchagents which have checked in with Gravwell.
func (c *Client) GetSearchAgentStatus() (types.SearchAgentStatus, error) {
	return c.get[types.SearchAgentStatus](searchAgentCheckinUrl())
}

// ReportScheduledSearchResults uploads a set of results for the scheduled search with the specified ID.
func (c *Client) ReportScheduledSearchResults(id string, results types.ScheduledSearchResults) error {
	return c.postStaticURL(scheduledSearchesIdResultsUrl(id), results, nil)
}

// GetScheduledSearchResults retrieves the most recent results for the specified scheduled search
func (c *Client) GetScheduledSearchResults(id string) (results types.ScheduledSearchResults, err error) {
	return c.get[types.ScheduledSearchResults](scheduledSearchesIdResultsUrl(id))
}

// ClearScheduledSearchResults deletes all results for the specified scheduled search
func (c *Client) ClearScheduledSearchResults(id string) error {
	return c.delete(scheduledSearchesIdResultsUrl(id), false)
}

// DebugScheduledSearch requests an immediate debug run of the specified scheduled search.
func (c *Client) DebugScheduledSearch(id string, opts types.AutomationDebugRequest) error {
	return c.postStaticURL(scheduledSearchesIdDebugUrl(id), opts, nil)
}

// CancelScheduledSearch cancels any active run of the specified scheduled search.
func (c *Client) CancelScheduledSearch(id string) error {
	return c.delete(scheduledSearchesIdCancelUrl(id), false)
}

// CleanupScheduledSearches (admin-only) purges all deleted scheduled searches for all users.
func (c *Client) CleanupScheduledSearches() error {
	return c.delete(SCHEDULED_SEARCHES_URL, false)
}
