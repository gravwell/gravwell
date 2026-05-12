/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
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

// ListTemplates returns all templates accessible to the current user.
func (c *Client) ListTemplates(opts *types.QueryOptions) (ret types.TemplateListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(TEMPLATES_LIST_URL, opts, &ret)
	return
}

// ListAllTemplates (admin-only) returns all templates on the system.
func (c *Client) ListAllTemplates(opts *types.QueryOptions) (ret types.TemplateListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	err = c.postStaticURL(TEMPLATES_LIST_URL, opts, &ret)
	return
}

// GetTemplate returns a particular template.
func (c *Client) GetTemplate(id string) (types.Template, error) {
	var template types.Template
	err := c.getStaticURL(templateUrl(id), &template)
	return template, err
}

// GetTemplateEx returns a particular template. If the QueryOptions arg is
// not nil, applicable parameters (currently only IncludeDeleted) will
// be applied to the query.
func (c *Client) GetTemplateEx(id string, opts *types.QueryOptions) (types.Template, error) {
	var template types.Template
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err := c.getStaticURL(templateUrl(id), &template, ezParam("include_deleted", opts.IncludeDeleted))
	return template, err
}

// DeleteTemplate deletes a template by marking it deleted in the database.
func (c *Client) DeleteTemplate(id string) error {
	return c.deleteStaticURL(templateUrl(id), nil)
}

// PurgeTemplate deletes a template entirely, removing it from the database.
func (c *Client) PurgeTemplate(id string) error {
	return c.deleteStaticURL(templateUrl(id), nil, ezParam("purge", "true"))
}

// CreateTemplate creates a new template, returning the newly-created template.
func (c *Client) CreateTemplate(t types.Template) (result types.Template, err error) {
	err = c.postStaticURL(TEMPLATES_URL, t, &result)
	return
}

// UpdateTemplate modifies an existing template.
func (c *Client) UpdateTemplate(t types.Template) (result types.Template, err error) {
	err = c.methodStaticPushURL(http.MethodPut, templateUrl(t.ID), t, &result, nil, nil)
	return
}

// CleanupTemplates (admin-only) purges all deleted templates for all users.
func (c *Client) CleanupTemplates() error {
	return c.deleteStaticURL(TEMPLATES_URL, nil)
}
