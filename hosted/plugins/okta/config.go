/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package okta

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/hosted"
)

const (
	oktaTag              string        = `okta`
	oktaUserTag          string        = `okta-users`
	defaultEmptyLookback time.Duration = -7 * 24 * time.Hour

	defaultPageSize         = 100
	defaultRequestPerMinute = 60
	defaultRequestBurst     = 10
	defaultRequestTimeout   = 20 * time.Second
)

var (
	Tags []string = []string{oktaTag, oktaUserTag}
)

type Config struct {
	hosted.BaseConfig
	Request_Batch_Size int
	Request_Per_Minute int
	Request_Burst      int
	Domain             string
	Token              string `json:"-"`
}

func (c *Config) Verify() (err error) {
	if c.Request_Batch_Size <= 0 {
		c.Request_Batch_Size = defaultPageSize
	} else if c.Request_Batch_Size > 3000 {
		return errors.New("Request-Batch-Size must be < 3000")
	}
	if c.Request_Per_Minute <= 0 {
		c.Request_Per_Minute = defaultRequestPerMinute
	} else if c.Request_Per_Minute > 6000 {
		return errors.New("Requests-Per-Minute must be < 6000")
	}
	if c.Request_Burst <= 0 {
		c.Request_Burst = defaultRequestBurst
	}
	if c.Token == `` {
		return errors.New("missing okta authentication token")
	}
	if c.Domain == `` {
		return errors.New("missing okta domain")
	} else if !strings.HasSuffix(c.Domain, `okta.com`) {
		err = fmt.Errorf("%q is not an okta domain", c.Domain)
		return
	}
	if c.Ingester_UUID == `` {
		return errors.New("missing Ingester-UUID")
	} else if _, err = uuid.Parse(c.Ingester_UUID); err != nil {
		return fmt.Errorf("invalid Ingester-UUID %q %w", c.Ingester_UUID, err)
	}
	return
}
