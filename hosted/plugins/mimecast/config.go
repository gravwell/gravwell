/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mimecast

import (
	"errors"
	"fmt"
	"slices"

	"github.com/gravwell/gravwell/v3/hosted"
)

const (
	defaultHost                     = "https://api.services.mimecast.com"
	defaultLookback                 = 24
	defaultRequestsPerMinute        = 5
	defaultInterval                 = 300
	defaultIngesterUUIDStr   string = "e528af50-3ccf-41be-b930-78ae9e10648d"
)

type Config struct {
	hosted.BaseConfig
	hosted.MultiTagConfig
	hosted.PollingConfig
	Client_Id     string `json:"-"` // DO NOT send this when marshalling
	Client_Secret string `json:"-"` // DO NOT send this when marshalling
	Api           []Api
	Host          string
	Preprocessor  []string
}

var _ hosted.Config = (*Config)(nil) // compile time interface check

// Equal implements hosted.Config so the runner can decide whether a config reload
// actually changed anything for this ingester.
func (c *Config) Equal(ncp any) bool {
	nc, ok := hosted.EqualTarget[Config](ncp)
	if c == nil || !ok {
		return false
	}
	return c.BaseConfig == nc.BaseConfig &&
		c.MultiTagConfig == nc.MultiTagConfig &&
		c.PollingConfig == nc.PollingConfig &&
		c.Client_Id == nc.Client_Id &&
		c.Client_Secret == nc.Client_Secret &&
		c.Host == nc.Host &&
		slices.Equal(c.Api, nc.Api) &&
		slices.Equal(c.Preprocessor, nc.Preprocessor)
}

func (c *Config) Verify() error {
	c.ApplyDefaultIngesterUUID(defaultIngesterUUIDStr)

	if c.Host == "" {
		c.Host = defaultHost
	}
	c.PollingConfig.ApplyDefaults(defaultLookback, defaultRequestsPerMinute,
		defaultInterval)
	if c.Client_Id == "" {
		return errors.New("Client-Id not specified")
	}
	if c.Client_Secret == "" {
		return errors.New("Client-Secret not specified")
	}
	for _, api := range c.Api {
		if _, supported := SIEMApiEvents[api]; !supported && api != AuditApi {
			return fmt.Errorf("API '%s' is not supported", api)
		}
	}
	if c.Tag_Name != "" && len(c.Api) > 1 {
		return fmt.Errorf("Tag-Name %q is only supported when specifying a single API",
			c.Tag_Name)
	}
	if err := c.MultiTagConfig.ValidateTags(); err != nil {
		return err
	}

	if err := c.BaseConfig.Verify(); err != nil {
		return err
	}

	return nil
}

func (c *Config) Tags() (tags []string) {
	for _, api := range c.Api {
		tags = append(tags, api.Tag(c.Tag_Name, c.Tag_Prefix))
	}
	return
}
