/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package msgraph

import (
	"cmp"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const (
	defaultLookbackHours          = 168 // 7 days
	defaultRequestsPerMin         = 10
	defaultRequestIntervalSeconds = 30
	defaultGraphHost              = "https://graph.microsoft.com"
	defaultAuthHost               = "https://login.microsoftonline.com"
)

type ErrInvalidConfigValue struct {
	Field   string
	Message string
}

func (err ErrInvalidConfigValue) Error() string {
	if err.Field != "" {
		return fmt.Sprintf("field %q invalid: %s", err.Field, err.Message)
	}
	return err.Message
}

// Config is the hosted plugin configuration for the MS Graph Security ingester.
type Config struct {
	Ingester_UUID       string
	Tenant_ID           string
	Client_ID           string
	Client_Secret       string `json:"-"`
	Content_Type        []ContentType
	Tag_Name            string // Single tag override. Only valid with one content type.
	Tag_Prefix          string // Prefix for auto-generated tags.
	Lookback            int    // In hours.
	Requests_Per_Minute int
	Request_Interval    int // In seconds between poll cycles.
	Graph_Host          string
	Auth_Host           string
}

// Verify validates the configuration has correct values set. It also sets defaults for values that can be defaulted.
// Returns all validation errors if there are multiple.
func (c *Config) Verify() error {
	var errs []error
	if c.Tenant_ID == "" {
		errs = append(errs, ErrInvalidConfigValue{Field: "Tenant-ID", Message: "not specified"})
	}
	if c.Client_ID == "" {
		errs = append(errs, ErrInvalidConfigValue{Field: "Client-ID", Message: "not specified"})
	}
	if c.Client_Secret == "" {
		errs = append(errs, ErrInvalidConfigValue{Field: "Client-Secret", Message: "not specified"})
	}
	if len(c.Content_Type) == 0 {
		errs = append(errs, ErrInvalidConfigValue{Field: "Content-Type", Message: "at least one value is required"})
	}
	for _, ct := range c.Content_Type {
		switch ct {
		case ContentAlerts, ContentSecureScores, ContentControlProfiles:
			continue
		default:
			errs = append(errs, ErrInvalidConfigValue{Field: "Content-Type", Message: fmt.Sprintf("unsupported value %q", string(ct))})
		}
	}
	if c.Tag_Name != "" && len(c.Content_Type) > 1 {
		errs = append(errs, ErrInvalidConfigValue{Message: "Tag-Name can only be used with a single Content-Type"})
	}
	if c.Tag_Name != "" && c.Tag_Prefix != "" {
		errs = append(errs, ErrInvalidConfigValue{Message: "Tag-Name and Tag-Prefix cannot be used together"})
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	c.Lookback = cmp.Or(c.Lookback, defaultLookbackHours)
	c.Requests_Per_Minute = cmp.Or(c.Requests_Per_Minute, defaultRequestsPerMin)
	c.Request_Interval = cmp.Or(c.Request_Interval, defaultRequestIntervalSeconds)
	c.Graph_Host = cmp.Or(c.Graph_Host, defaultGraphHost)
	c.Auth_Host = cmp.Or(c.Auth_Host, defaultAuthHost)

	return nil
}

// UUID returns the parsed ingester UUID, or uuid.Nil if empty/invalid UUID.
func (c *Config) UUID() uuid.UUID {
	if c.Ingester_UUID != "" {
		if val, err := uuid.Parse(c.Ingester_UUID); err == nil {
			return val
		}
	}
	return uuid.Nil
}

// Tags returns all of the tags we're configured for based off the configured content types.
func (c *Config) Tags() []string {
	tags := make([]string, 0, len(c.Content_Type))
	for _, ct := range c.Content_Type {
		tags = append(tags, ct.Tag(c.Tag_Name, c.Tag_Prefix))
	}
	return tags
}
