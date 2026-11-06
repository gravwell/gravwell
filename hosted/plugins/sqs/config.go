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

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/sqs_common"
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
	Queue_URL         string
	Region            string
	Endpoint          string
	Credentials_Type  string
	AKID              string
	Secret            string `json:"-"` // DO NOT send this when marshalling
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
	return nil
}

func (c *Config) Tags() []string {
	return []string{c.ResolveTag(entry.DefaultTagName)}
}
