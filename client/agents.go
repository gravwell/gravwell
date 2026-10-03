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

// ListAgents returns all agents accessible to the current user.
func (c *Client) ListAgents(opts *types.QueryOptions) (ret types.AgentListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	return c.post[types.QueryOptions, types.AgentListResponse](LIST_AGENTS_URL, opts)
}

// ListAllAgents (admin-only) returns all agents on the system.
func (c *Client) ListAllAgents(opts *types.QueryOptions) (ret types.AgentListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	return c.post[types.QueryOptions, types.AgentListResponse](LIST_AGENTS_URL, opts)
}

// GetAgent returns a particular agent.
func (c *Client) GetAgent(id string) (types.Agent, error) {
	return c.GetAgentEx(id, GetOptions{})
}

// GetAgentEx returns a particular agent, modified by opts.
func (c *Client) GetAgentEx(id string, opts GetOptions) (types.Agent, error) {
	return c.get[types.Agent](agentsIdUrl(id), opts.params()...)
}

// DeleteAgent deletes an agent by marking it deleted in the database.
func (c *Client) DeleteAgent(id string) error {
	return c.delete(agentsIdUrl(id), false)
}

// PurgeAgent deletes an agent entirely, removing it from the database.
func (c *Client) PurgeAgent(id string) error {
	return c.delete(agentsIdUrl(id), true)
}

// CreateAgent creates a new agent, returning the newly-created agent.
func (c *Client) CreateAgent(a types.Agent) (result types.Agent, err error) {
	return c.post[types.Agent, types.Agent](AGENTS_URL, &a)
}

// UpdateAgent modifies an existing agent and returns the complete, updated struct.
func (c *Client) UpdateAgent(ID string, p types.AgentPatch) (updated types.Agent, err error) {
	if ID == "" {
		return types.Agent{}, ErrEmptyID
	}
	return c.patch[types.AgentPatch, types.Agent](agentsIdUrl(ID), p)
}

// CleanupAgents (admin-only) purges all deleted agents for all users.
func (c *Client) CleanupAgents() error {
	return c.delete(AGENTS_URL, false)
}

// ListAgentSkills returns all agent skills accessible to the current user.
func (c *Client) ListAgentSkills(opts *types.QueryOptions) (ret types.AgentSkillListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	return c.post[types.QueryOptions, types.AgentSkillListResponse](LIST_AGENT_SKILLS_URL, opts)
}

// ListAllAgentSkills (admin-only) returns all agent skills on the system.
func (c *Client) ListAllAgentSkills(opts *types.QueryOptions) (ret types.AgentSkillListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	return c.post[types.QueryOptions, types.AgentSkillListResponse](LIST_AGENT_SKILLS_URL, opts)
}

// GetAgentSkill returns a particular agent skill.
func (c *Client) GetAgentSkill(id string) (types.AgentSkill, error) {
	return c.GetAgentSkillEx(id, GetOptions{})
}

// GetAgentSkillEx returns a particular agent skill, modified by opts.
func (c *Client) GetAgentSkillEx(id string, opts GetOptions) (types.AgentSkill, error) {
	return c.get[types.AgentSkill](agentSkillsIdUrl(id), opts.params()...)
}

// DeleteAgentSkill deletes an agent skill by marking it deleted in the database.
func (c *Client) DeleteAgentSkill(id string) error {
	return c.delete(agentSkillsIdUrl(id), false)
}

// PurgeAgentSkill deletes an agent skill entirely, removing it from the database.
func (c *Client) PurgeAgentSkill(id string) error {
	return c.delete(agentSkillsIdUrl(id), true)
}

// CreateAgentSkill creates a new agent skill, returning the newly-created skill.
func (c *Client) CreateAgentSkill(s types.AgentSkill) (result types.AgentSkill, err error) {
	return c.post[types.AgentSkill, types.AgentSkill](AGENT_SKILLS_URL, &s)
}

// UpdateAgentSkill modifies an existing agent skill and returns the complete, updated struct.
func (c *Client) UpdateAgentSkill(ID string, p types.AgentSkillPatch) (updated types.AgentSkill, err error) {
	if ID == "" {
		return types.AgentSkill{}, ErrEmptyID
	}
	return c.patch[types.AgentSkillPatch, types.AgentSkill](agentSkillsIdUrl(ID), p)
}

// CleanupAgentSkills (admin-only) purges all deleted agent skills for all users.
func (c *Client) CleanupAgentSkills() error {
	return c.delete(AGENT_SKILLS_URL, false)
}
