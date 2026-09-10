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

// ListTemplates returns all templates accessible to the current user.
func (c *Client) ListTemplates(opts *types.QueryOptions) (ret types.TemplateListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	return c.post[types.QueryOptions, types.TemplateListResponse](TEMPLATES_LIST_URL, opts)
}

// ListAllTemplates (admin-only) returns all templates on the system.
func (c *Client) ListAllTemplates(opts *types.QueryOptions) (ret types.TemplateListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	return c.post[types.QueryOptions, types.TemplateListResponse](TEMPLATES_LIST_URL, opts)
}

// GetTemplate returns a particular template.
func (c *Client) GetTemplate(id string) (types.Template, error) {
	return c.GetTemplateEx(id, GetOptions{})
}

// GetTemplateEx returns a particular template, modified by opts.
func (c *Client) GetTemplateEx(id string, opts GetOptions) (types.Template, error) {
	return c.get[types.Template](templateUrl(id), opts.params()...)
}

// DeleteTemplate deletes a template by marking it deleted in the database.
func (c *Client) DeleteTemplate(id string) error {
	return c.delete(templateUrl(id), false)
}

// PurgeTemplate deletes a template entirely, removing it from the database.
func (c *Client) PurgeTemplate(id string) error {
	return c.delete(templateUrl(id), true)
}

// CreateTemplate creates a new template, returning the newly-created template.
func (c *Client) CreateTemplate(t types.Template) (result types.Template, err error) {
	return c.post[types.Template, types.Template](TEMPLATES_URL, &t)
}

// UpdateTemplate modifies an existing template and returns the complete, updated struct.
func (c *Client) UpdateTemplate(ID string, p types.TemplatePatch) (updated types.Template, err error) {
	if ID == "" {
		return types.Template{}, ErrEmptyID
	}
	return c.patch[types.TemplatePatch, types.Template](templateUrl(ID), p)
}

// CleanupTemplates (admin-only) purges all deleted templates for all users.
func (c *Client) CleanupTemplates() error {
	return c.delete(TEMPLATES_URL, false)
}
