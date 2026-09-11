/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package jamf is a hosted ingester plugin that polls the Jamf Pro API's
// computers-inventory endpoint for records that were modified in a given
// time window, using general.reportDate as the tracking cursor. Each
// configured section is emitted to its own tag, with the GENERAL section's
// data folded into every entry for context.
package jamf

import (
	"cmp"
	"errors"
	"fmt"
	"strings"
	"slices"

	"github.com/gravwell/gravwell/v3/hosted"
)

const (
	defaultIngesterUUIDStr   = "7e468cc4-ab10-4b33-b066-90eb4905980b"
	defaultTagPrefix         = `jamf`
	defaultPageSize          = 100
	defaultLookback          = 1   // hours
	defaultRequestsPerMinute = 60  // token bucket for the inventory + oauth calls
	defaultInterval          = 600 // seconds (10 minutes)

	// pollBufferSeconds keeps the window from reaching all the way up to
	// "now", since Jamf can take a few seconds to finish writing a record
	// after a device checks in. Without this, records near the trailing
	// edge of a window could be missed entirely.
	pollBufferSeconds = 60
)

// generalSectionName is the Jamf computers-inventory section that's always
// requested (for reportDate/id) and folded into every other section's
// entry. It only needs a place in Sections if you also want a dedicated
// general-only stream.
const generalSectionName = "GENERAL"

// defaultSections lists the computers-inventory sections we request data
// for, and emit a separate tag for, if the config doesn't specify any.
var defaultSections = []string{"DISK_ENCRYPTION", "STORAGE"}

type Config struct {
	hosted.BaseConfig
	hosted.MultiTagConfig
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
		c.Tag_Prefix == nc.Tag_Prefix &&
		c.MultiTagConfig == nc.MultiTagConfig &&
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

	if len(c.Sections) == 0 {
		c.Sections = append([]string{}, defaultSections...)
	}
	for i, s := range c.Sections {
		c.Sections[i] = strings.ToUpper(strings.TrimSpace(s))
	}
	if dup := firstDuplicate(c.Sections); dup != "" {
		return fmt.Errorf("Sections lists %q more than once", dup)
	}

	if err := c.MultiTagConfig.ValidateTags(); err != nil {
		return err
	}
	if c.Tag_Name != "" && len(c.Sections) > 1 {
		return errors.New("Tag-Name can only be used with a single Sections entry; use Tag-Prefix instead")
	}

	c.PollingConfig.ApplyDefaults(defaultLookback, defaultRequestsPerMinute, defaultInterval)

	return nil
}

func firstDuplicate(vals []string) string {
	seen := make(map[string]bool, len(vals))
	for _, v := range vals {
		if seen[v] {
			return v
		}
		seen[v] = true
	}
	return ""
}

// requestSections returns the full set of Jamf inventory sections to
// request from the API for a page: every configured section plus GENERAL
// (deduplicated). GENERAL is always requested since it carries the
// reportDate/id used for every emitted entry, regardless of whether the
// operator listed it explicitly in Sections.
func (c *Config) requestSections() []string {
	out := make([]string, 0, len(c.Sections)+1)
	out = append(out, generalSectionName)
	for _, s := range c.Sections {
		if s != generalSectionName {
			out = append(out, s)
		}
	}
	return out
}

// tagForSection returns the tag that a configured section's entries should
// be written under: Tag-Name verbatim if set (only valid for a single
// configured section), otherwise "<Tag-Prefix or 'jamf'>_<section, lowercased>".
func (c *Config) tagForSection(section string) string {
	if c.Tag_Name != "" {
		return c.Tag_Name
	}
	prefix := cmp.Or(c.Tag_Prefix, defaultTagPrefix)
	return prefix + "_" + strings.ToLower(section)
}

// Tags returns every tag this plugin instance can write to, one per
// configured section.
func (c *Config) Tags() []string {
	tags := make([]string, 0, len(c.Sections))
	for _, s := range c.Sections {
		tags = append(tags, c.tagForSection(s))
	}
	return tags
}
