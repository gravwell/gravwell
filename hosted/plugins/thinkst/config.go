/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package thinkst

import (
	"errors"
	"fmt"
	"slices"

	"github.com/gravwell/gravwell/v3/hosted"
)

const (
	defaultLookback          = 24 // hours
	defaultRequestsPerMinute = 60
	defaultInterval          = 60 // seconds

	defaultIngesterUUIDStr = "c5a51d2e-0342-4b46-89da-676192ed5699"
)

// Api identifies which Thinkst Canary endpoint an instance polls.
type Api string

const (
	IncidentApi Api = "incident"
	AuditApi    Api = "audit"
)

// IsValidApi reports whether a is a supported Api value.
func IsValidApi(a Api) bool {
	switch a {
	case IncidentApi, AuditApi:
		return true
	}
	return false
}

// Tag resolves the tag entries fetched from this Api should be written to:
// an explicit tag override if set, otherwise prefix-api, otherwise the api
// name itself.
func (a Api) Tag(tag, prefix string) string {
	if tag != "" {
		return tag
	}
	if prefix != "" {
		return prefix + "-" + string(a)
	}
	return string(a)
}

type Config struct {
	hosted.BaseConfig
	hosted.MultiTagConfig
	hosted.PollingConfig
	Domain string
	Token  string `json:"-"` // DO NOT send this when marshalling
	Api    []Api
}

var _ hosted.Config = (*Config)(nil) // compile time interface check

// Equal implements hosted.Config so the runner can tell whether a config
// reload actually changed anything for this ingester.
func (c *Config) Equal(ncp any) bool {
	nc, ok := hosted.EqualTarget[Config](ncp)
	if c == nil || !ok {
		return false
	}
	return c.BaseConfig == nc.BaseConfig &&
		c.MultiTagConfig == nc.MultiTagConfig &&
		c.PollingConfig == nc.PollingConfig &&
		c.Domain == nc.Domain &&
		c.Token == nc.Token &&
		slices.Equal(c.Api, nc.Api)
}

func (c *Config) Verify() error {
	c.ApplyDefaultIngesterUUID(defaultIngesterUUIDStr)
	c.PollingConfig.ApplyDefaults(defaultLookback, defaultRequestsPerMinute, defaultInterval)

	if c.Domain == "" {
		return errors.New("Domain not specified")
	}
	if c.Token == "" {
		return errors.New("Token not specified")
	}
	if len(c.Api) == 0 {
		return errors.New("Api not specified")
	}
	for _, api := range c.Api {
		if !IsValidApi(api) {
			return fmt.Errorf("Api %q is not valid, must be %q or %q", api, IncidentApi, AuditApi)
		}
	}
	if c.Tag_Name != "" && len(c.Api) > 1 {
		return fmt.Errorf("Tag-Name %q is only supported when specifying a single Api", c.Tag_Name)
	}
	if err := c.MultiTagConfig.ValidateTags(); err != nil {
		return err
	}
	if err := c.BaseConfig.Verify(); err != nil {
		return err
	}
	return nil
}

// Tags returns the tag each configured Api resolves to, via Api.Tag.
func (c *Config) Tags() (tags []string) {
	for _, api := range c.Api {
		tags = append(tags, api.Tag(c.Tag_Name, c.Tag_Prefix))
	}
	return
}
