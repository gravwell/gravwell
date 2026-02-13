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
	"fmt"
	"net/http"

	"github.com/gravwell/gravwell/v4/client/types"
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

// CreateScheduledSearchFromObject makes a new scheduled search and returns the ID. The parameters are:
//
// - s: A scheduled search object.
func (c *Client) CreateScheduledSearch(s types.ScheduledSearch) (int32, error) {
	if s.SearchString != "" && s.SearchReference != "" {
		return 0, fmt.Errorf("cannot use both SearchReference and SearchString in CreateScheduledSearch")
	}
	var resp int32
	if err := c.postStaticURL(scheduledSearchUrl(), s, &resp); err != nil {
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
func (c *Client) DeleteScheduledSearch(id string) error {
	return c.deleteStaticURL(scheduledSearchIdUrl(id), nil)
}

// GetScheduledSearch returns the scheduled search with the given ID.
func (c *Client) GetScheduledSearch(id string) (types.ScheduledSearch, error) {
	var search types.ScheduledSearch
	err := c.getStaticURL(scheduledSearchIdUrl(id), &search)
	return search, err
}

// ScheduledSearchCheckin (admin-only) informs the webserver that the search agent is active.
func (c *Client) ScheduledSearchCheckin(cfg types.SearchAgentConfig) error {
	return c.putStaticURL(scheduledSearchCheckinUrl(), cfg)
}

// GetSearchAgentCheckin finds out when the most recent searchagent checkin was.
func (c *Client) GetSearchAgentCheckin() (ci types.SearchAgentCheckin, err error) {
	err = c.getStaticURL(scheduledSearchCheckinUrl(), &ci)
	return
}

// ClearScheduledSearchError clears the error field on the specified scheduled search.
func (c *Client) ClearScheduledSearchError(id string) error {
	return c.deleteStaticURL(scheduledSearchErrorIdUrl(id), nil)
}

// ClearScheduledSearchState clears state variables on the specified scheduled search.
func (c *Client) ClearScheduledSearchState(id string) error {
	return c.deleteStaticURL(scheduledSearchStateIdUrl(id), nil)
}

// ParseScheduledScript asks the API to parse a script given an ID
// if there is no error line and column will have a return value of 0
// if there is an error, err will be populated and potentially a line and column if the error was in the script
func (c *Client) ParseScheduledScript(data string, lang types.ScriptLang) (line, column int, err error) {
	if err = lang.Valid(); err != nil {
		return
	}
	var resp types.ScheduledScriptParseResponse
	req := types.ScheduledScriptParseRequest{
		Version: lang,
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
