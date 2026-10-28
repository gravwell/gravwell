/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package sqs

import (
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/sqs_common"
)

const (
	Name                   string = `sqs`
	ID                     string = `sqs.ingesters.gravwell.io`
	Version                string = `1.0.0`
	defaultIngesterUUIDStr string = "dc482963-7190-4014-8f7f-cdcbe8361bff"
)

type Config struct {
	hosted.BaseConfig
	hosted.SingleTagConfig
	Queue_URL        string `dynamic:"required"`
	Region           string `dynamic:"required"`
	Endpoint         string
	Credentials_Type string `dynamic:"enum=static|environment|ec2role"`
	// AKID and Secret are only needed for static credentials.  An empty Credentials-Type
	// means static, see sqs_common.GetCredentials, so the condition has to match that too
	// or leaving the type alone would quietly drop the requirement.
	AKID              string `dynamic:"requiredif=Credentials-Type:|static"`
	Secret            string `json:"-" dynamic:"secret,requiredif=Credentials-Type:|static"` // DO NOT send this when marshalling
	Ignore_Timestamps bool
}

func (c *Config) Verify() error {
	c.ApplyDefaultIngesterUUID(defaultIngesterUUIDStr)

	if c.Queue_URL == "" {
		return errors.New("Queue-URL not specified")
	}
	if c.Region == "" {
		return errors.New("Region not specified")
	}
	if _, err := sqs_common.GetCredentials(c.Credentials_Type, c.AKID, c.Secret); err != nil {
		return fmt.Errorf("invalid credentials: %w", err)
	}
	if err := c.BaseConfig.Verify(); err != nil {
		return err
	}
	// last, so the tags checked are the ones this config will really write to
	return hosted.VerifyTags(c)
}

func (c *Config) Tags() []string {
	return []string{c.ResolveTag(entry.DefaultTagName)}
}
