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

	"github.com/gravwell/gravwell/v4/client/types"
)

// ListScheduledScripts returns scheduled scripts the user has access to.
func (c *Client) ListScheduledScripts(opts *types.QueryOptions) (scripts types.ScheduledScriptListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	if err = c.postStaticURL(SCHEDULED_SCRIPT_LIST_URL, opts, &scripts); err != nil {
		return
	}
	return
}

// ListAllScheduledScripts returns all scheduled scripts on the system (for admins).
func (c *Client) ListAllScheduledScripts(opts *types.QueryOptions) (scripts types.ScheduledScriptListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	if err = c.postStaticURL(SCHEDULED_SCRIPT_LIST_URL, opts, &scripts); err != nil {
		return
	}
	return
}

// GetScheduledScript returns the scheduled script with the given ID.
func (c *Client) GetScheduledScript(id string) (types.ScheduledScript, error) {
	var script types.ScheduledScript
	err := c.getStaticURL(scheduledScriptIdUrl(id), &script)
	return script, err
}

// GetScheduledScriptEx returns a particular scheduled script. If the QueryOptions arg is
// not nil, applicable parameters (currently only IncludeDeleted) will
// be applied to the query.
func (c *Client) GetScheduledScriptEx(id string, opts *types.QueryOptions) (types.ScheduledScript, error) {
	var script types.ScheduledScript
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err := c.getStaticURL(scheduledScriptIdUrl(id), &script, ezParam("include_deleted", opts.IncludeDeleted))
	return script, err
}

// DeleteScheduledScript removes the specified scheduled script.
func (c *Client) DeleteScheduledScript(id string) error {
	return c.deleteStaticURL(scheduledScriptIdUrl(id), nil)
}

// PurgeScheduledScript permanently removes the specified scheduled script.
func (c *Client) PurgeScheduledScript(id string) error {
	return c.deleteStaticURL(scheduledScriptIdUrl(id), nil, ezParam("purge", "true"))
}

// CreateScheduledScript makes a new scheduled script.
func (c *Client) CreateScheduledScript(spec types.ScheduledScript) (result types.ScheduledScript, err error) {
	err = c.postStaticURL(scheduledScriptUrl(), spec, &result)
	return
}

// UpdateScheduledScript is used to modify an existing scheduled script.
func (c *Client) UpdateScheduledScript(ss types.ScheduledScript) error {
	return c.putStaticURL(scheduledScriptIdUrl(ss.ID), ss)
}

// UpdateScheduledScriptResults is used to update the scheduled script after it has been
// run. It only updates the PersistentMaps, LastRun, LastRunDuration, LastScriptIDs,
// and LastError fields
func (c *Client) UpdateScheduledScriptResults(ss types.ScheduledScript) error {
	return c.putStaticURL(scheduledScriptResultsIdUrl(ss.ID), ss)
}

// ParseScheduledScript asks the API to parse a script given an ID.
// If there is no error, line and column will have a return value of 0.
// If there is an error, err will be populated and potentially a line and column if the error was in the script.
func (c *Client) ParseScheduledScript(data string, lang types.ScriptLang) (line, column int, err error) {
	if err = lang.Valid(); err != nil {
		return
	}
	var resp types.ScheduledScriptParseResponse
	req := types.ScheduledScriptParseRequest{
		ScriptLanguage: lang,
		Script:         data,
	}
	if err = c.methodStaticPushURL(http.MethodPut, scheduledScriptParseUrl(), req, &resp, nil, nil); err != nil {
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

// ReportScheduledScriptResults uploads a set of results for the scheduled script with the specified ID.
func (c *Client) ReportScheduledScriptResults(id string, results types.ScheduledScriptResults) error {
	return c.postStaticURL(scheduledScriptResultsIdUrl(id), results, nil)
}

// GetScheduledScriptResults retrieves the most recent results for the specified scheduled script
func (c *Client) GetScheduledScriptResults(id string) (results types.ScheduledScriptResults, err error) {
	err = c.getStaticURL(scheduledScriptResultsIdUrl(id), &results)
	return
}

// ClearScheduledScriptResults deletes all results for the specified scheduled script
func (c *Client) ClearScheduledScriptResults(id string) error {
	return c.deleteStaticURL(scheduledScriptResultsIdUrl(id), nil)
}

// DebugScheduledScript requests an immediate debug run of the specified scheduled script.
func (c *Client) DebugScheduledScript(id string, opts types.AutomationDebugRequest) error {
	return c.postStaticURL(scheduledScriptDebugIdUrl(id), opts, nil)
}

// CancelScheduledScript cancels any active run of the specified scheduled script.
func (c *Client) CancelScheduledScript(id string) error {
	return c.deleteStaticURL(scheduledScriptCancelIdUrl(id), nil)
}
