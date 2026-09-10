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

	"github.com/gravwell/gravwell/v3/hosted"
)

const (
	defaultLookback          = 24 // hours
	defaultRequestsPerMinute = 60
	defaultInterval          = 60 // seconds
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

type Config struct {
	hosted.BaseConfig
	hosted.SingleTagConfig
	hosted.PollingConfig
	Domain string
	Token  string `json:"-"` // DO NOT send this when marshalling
	Api    Api
}

func (c *Config) Verify() error {
	c.PollingConfig.ApplyDefaults(defaultLookback, defaultRequestsPerMinute, defaultInterval)
	if c.Tag_Name == "" {
		return errors.New("Tag-Name not specified")
	}
	if c.Domain == "" {
		return errors.New("Domain not specified")
	}
	if c.Token == "" {
		return errors.New("Token not specified")
	}
	if !IsValidApi(c.Api) {
		return fmt.Errorf("Api %q is not valid, must be %q or %q", c.Api, IncidentApi, AuditApi)
	}
	return nil
}

func (c *Config) Tags() []string {
	return []string{c.Tag_Name}
}
