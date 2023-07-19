/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"errors"
	"net/http"
	"time"

	"github.com/gravwell/gravwell/v3/client/types"
)

// GetScheduledSearchList returns scheduled searches the user has access to.
func (c *Client) GetScheduledSearchList() ([]types.ScheduledSearch, error) {
	var searches []types.ScheduledSearch
	if err := c.getStaticURL(scheduledSearchUrl(), &searches); err != nil {
		return nil, err
	}
	return searches, nil
}

// GetAllScheduledSearches (admin-only) returns all scheduled searches on the system.
func (c *Client) GetAllScheduledSearches() ([]types.ScheduledSearch, error) {
	var searches []types.ScheduledSearch
	if err := c.getStaticURL(scheduledSearchAllUrl(), &searches); err != nil {
		return nil, err
	}
	return searches, nil
}

// CreateScheduledSearch makes a new scheduled search and returns the ID. The parameters are:
//
// - name: the search name.
//
// - description: the search description.
//
// - schedule: a cron-format schedule on which to execute the search.
//
// - searchquery: a valid search query string.
//
// - duration: the amount of time over which the query should be run.
func (c *Client) CreateScheduledSearch(name, description, schedule, searchquery string, duration time.Duration, groups []int32) (int32, error) {
	ss := types.ScheduledSearch{
		Groups:       groups,
		Name:         name,
		Description:  description,
		Schedule:     schedule,
		SearchString: searchquery,
		Duration:     int64(duration.Seconds()),
	}
	var resp int32
	if err := c.postStaticURL(scheduledSearchUrl(), ss, &resp); err != nil {
		return 0, err
	}
	return resp, nil
}

// Create a scheduled search that executes a script instead of a search. The parameters are:
//
// - name: the search name.
//
// - description: the search description.
//
// - schedule: a cron-format schedule on which to execute the search.
//
// - script: a valid anko script.
//
// - groups: an optional array of groups which should be able to access this object.
//
// - lang: the language of scheduled script (anko, go)
func (c *Client) CreateScheduledScript(name, description, schedule, script string, lang types.ScriptLang, groups []int32) (int32, error) {
	if err := lang.Valid(); err != nil {
		return -1, err
	}
	ss := types.ScheduledSearch{
		Groups:         groups,
		Name:           name,
		Description:    description,
		Schedule:       schedule,
		Script:         script,
		ScriptLanguage: lang,
	}
	var resp int32
	if err := c.postStaticURL(scheduledSearchUrl(), ss, &resp); err != nil {
		return 0, err
	}
	return resp, nil
}

// UpdateScheduledSearchResults is used to update the scheduled search after it has been
// run. It only updates the PersistentMaps, LastRun, LastRunDuration, LastSearchIDs,
// and LastError fields
func (c *Client) UpdateScheduledSearchResults(ss types.ScheduledSearch) error {
	return c.putStaticURL(scheduledSearchResultsIdUrl(ss.ID), ss)
}

// UpdateScheduledSearch is used to modify an existing scheduled search.
func (c *Client) UpdateScheduledSearch(ss types.ScheduledSearch) error {
	return c.putStaticURL(scheduledSearchIdUrl(ss.ID), ss)
}

// DeleteScheduledSearch removes the specified scheduled search.
func (c *Client) DeleteScheduledSearch(id int32) error {
	return c.deleteStaticURL(scheduledSearchIdUrl(id), nil)
}

// GetScheduledSearch returns the scheduled search with the given ID.
func (c *Client) GetScheduledSearch(id int32) (types.ScheduledSearch, error) {
	var search types.ScheduledSearch
	err := c.getStaticURL(scheduledSearchIdUrl(id), &search)
	return search, err
}

// GetUserScheduledSearches returns all scheduled searches belonging to the specified user.
func (c *Client) GetUserScheduledSearches(uid int32) ([]types.ScheduledSearch, error) {
	var searches []types.ScheduledSearch
	if err := c.getStaticURL(scheduledSearchUserUrl(uid), &searches); err != nil {
		return nil, err
	}
	return searches, nil
}

// ClearUserScheduledSearches removes all scheduled searches belonging to the specified user
func (c *Client) ClearUserScheduledSearches(uid int32) error {
	return c.deleteStaticURL(scheduledSearchUserUrl(uid), nil)
}

// ScheduledSearchCheckin (admin-only) informs the webserver that the search agent is active.
func (c *Client) ScheduledSearchCheckin(cfg types.SearchAgentConfig) error {
	return c.putStaticURL(scheduledSearchCheckinUrl(), cfg)
}

// ClearScheduledSearchError clears the error field on the specified scheduled search.
func (c *Client) ClearScheduledSearchError(id int32) error {
	return c.deleteStaticURL(scheduledSearchErrorIdUrl(id), nil)
}

// ClearScheduledSearchState clears state variables on the specified scheduled search.
func (c *Client) ClearScheduledSearchState(id int32) error {
	return c.deleteStaticURL(scheduledSearchStateIdUrl(id), nil)
}

// ParseScheduledScript asks the API to parse a script given an ID
// if there is no error line and column will have a return value of 0
// if there is an error, err will be populated and potentially a line and column if the error was in the script
func (c *Client) ParseScheduledScript(data string, lang types.ScriptLang) (line, column int, err error) {
	if err = lang.Valid(); err != nil {
		return
	}
	var resp types.ScheduledSearchParseResponse
	req := types.ScheduledSearchParseRequest{
		Version: int(lang),
		Script:  data,
	}
	if err = c.methodStaticPushURL(http.MethodPut, scheduledSearchParseUrl(), req, &resp, nil, nil); err != nil {
		return
	}
	if resp.OK {
		return //all is good
	}

	//if the parse failed but we don't have an error, set something
	if len(resp.Error) == 0 {
		resp.Error = `Unknown parse error`

	}
	line, column = resp.ErrorLine, resp.ErrorColumn
	err = errors.New(resp.Error)
	return
}
