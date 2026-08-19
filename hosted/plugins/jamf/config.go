/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package jamf is a hosted ingester plugin that polls the Jamf Pro API's
// computers-inventory endpoint for records that were modified in a given
// time window, using general.reportDate as the tracking cursor.
package jamf

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v3/hosted"
)

const (
	defaultIngesterUUIDStr string = "7e468cc4-ab10-4b33-b066-90eb4905980b"

	defaultTag               = `jamf`
	defaultPageSize          = 100
	defaultLookback          = 1   // hours
	defaultRequestsPerMinute = 60  // token bucket for the inventory + oauth calls
	defaultInterval          = 600 // seconds (10 minutes)

	// PollBufferSeconds keeps the window from reaching all the way up to
	// "now", since Jamf can take a few seconds to finish writing a record
	// after a device checks in. Without this, records near the trailing
	// edge of a window could be missed entirely.
	pollBufferSeconds = 60
)

const sectionGeneral string = "GENERAL"

// Sections lists the computers-inventory sections we'll request if the
// config doesn't specify any explicitly.
var defaultSections = []string{sectionGeneral}

type Config struct {
	hosted.BaseConfig
	hosted.SingleTagConfig
	hosted.PollingConfig

	Host          string
	Client_Id     string
	Client_Secret string `json:"-"` // DO NOT send this when marshalling
	Page_Size     int
	Sections      []string

	Insecure_Skip_TLS_Verify bool
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
		c.SingleTagConfig == nc.SingleTagConfig &&
		c.PollingConfig == nc.PollingConfig &&
		c.Host == nc.Host &&
		c.Client_Id == nc.Client_Id &&
		c.Client_Secret == nc.Client_Secret &&
		c.Page_Size == nc.Page_Size &&
		c.Insecure_Skip_TLS_Verify == nc.Insecure_Skip_TLS_Verify &&
		slices.Equal(c.Sections, nc.Sections)
}

func (c *Config) Verify() error {
	c.ApplyDefaultIngesterUUID(defaultIngesterUUIDStr)

	if c.Host == "" {
		return errors.New("Host not specified")
	}
	c.Host = strings.TrimRight(c.Host, "/")

	if c.Client_Id == "" {
		return errors.New("Client-Id not specified")
	}
	if c.Client_Secret == "" {
		return errors.New("Client-Secret not specified")
	}

	if c.Page_Size <= 0 {
		c.Page_Size = defaultPageSize
	} else if c.Page_Size > 1000 {
		return fmt.Errorf("Page-Size %d is too large, must be <= 1000", c.Page_Size)
	}

	// GENERAL should _always_ be in c.Sections.
	if len(c.Sections) == 0 {
		c.Sections = defaultSections
	} else if !slices.Contains(c.Sections, sectionGeneral) {
		c.Sections = append(c.Sections, sectionGeneral)
	}

	c.PollingConfig.ApplyDefaults(defaultLookback, defaultRequestsPerMinute, defaultInterval)

	return nil
}

func (c *Config) Tags() []string {
	return []string{c.ResolveTag(defaultTag)}
}
