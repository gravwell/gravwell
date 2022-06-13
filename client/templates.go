/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"encoding/json"
	"net/http"

	"github.com/gravwell/gravwell/v3/client/types"

	"github.com/google/uuid"
)

// ListTemplates returns a list of templates accessible to the current user.
func (c *Client) ListTemplates() (templates []types.WireUserTemplate, err error) {
	err = c.getStaticURL(templatesUrl(), &templates)
	return
}

// ListAllTemplates returns the list of all templates in the system, admin only API
func (c *Client) ListAllTemplates() (templates []types.WireUserTemplate, err error) {
	if !c.userDetails.Admin {
		err = ErrNotAdmin
	} else {
		c.SetAdminMode()
		if err = c.getStaticURL(templatesUrl(), &templates); err != nil {
			templates = nil
		}
		c.ClearAdminMode()
	}
	return
}

// NewTemplate creates a new template with the given GUID, name, description, contents.
// If guid is set to uuid.Nil, a random GUID will be chosen automatically.
func (c *Client) NewTemplate(guid uuid.UUID, name, description string, contents types.RawObject) (details types.WireUserTemplate, err error) {
	// Mash the content blob into an appropriate type
	var ct types.TemplateContents
	if err = json.Unmarshal(contents, &ct); err != nil {
		return
	}
	template := types.UserTemplate{GUID: guid, Contents: ct, Name: name, Description: description}
	err = c.methodStaticPushURL(http.MethodPost, templatesUrl(), template, &details)
	return
}

// GetTemplate returns a types.WireUserTemplate with the requested GUID.
// Because unique GUIDs are not enforced, the following precedence
// is used when selecting a template to return:
// 1. Templates owned by the user always have highest priority
// 2. Templates shared with a group to which the user belongs are next
// 3. Global templates are the lowest priority
func (c *Client) GetTemplate(guid uuid.UUID) (template types.WireUserTemplate, err error) {
	err = c.getStaticURL(templatesGuidUrl(guid), &template)
	return
}

// SetTemplate allows the owner of a template (or an admin) to update
// the contents of the template.
func (c *Client) SetTemplate(guid uuid.UUID, template types.WireUserTemplate) (details types.WireUserTemplate, err error) {
	err = c.methodStaticPushURL(http.MethodPut, templatesGuidUrl(guid), template, &details)
	return
}

// DeleteTemplate deletes the template with the specified GUID
func (c *Client) DeleteTemplate(guid uuid.UUID) (err error) {
	err = c.deleteStaticURL(templatesGuidUrl(guid), nil)
	return
}
