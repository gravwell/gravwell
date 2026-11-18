/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package okta

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
)

const (
	defaultIngesterUUIDStr string = "55af6d4e-3d04-431b-b860-b15b921a46c5"

	oktaTag              string        = `okta`              // this is backed by the kit, do not change
	oktaUserTag          string        = `okta-users`        // this is expected by the kit, do not change
	defaultEmptyLookback time.Duration = -7 * 24 * time.Hour // if we have no previous state we will go back 7 days

	// some sane defaults
	defaultPageSize         = 100
	defaultRequestPerMinute = 60
	defaultRequestBurst     = 10
	defaultRequestTimeout   = 20 * time.Second
)

var (
	Tags []string = []string{oktaTag, oktaUserTag}

	// parent domains of Okta org URLs, as listed by the org picker at login.okta.com
	oktaDomainSuffixes = []string{`.okta.com`, `.okta-emea.com`, `.okta-gov.com`, `.okta.mil`, `.oktapreview.com`}
)

type Config struct {
	hosted.BaseConfig
	Request_Batch_Size int // how many entries do we request per HTTP request
	Request_Per_Minute int // what is our basic request rate
	Request_Burst      int // leaky bucket burstability
	Domain             string
	Token              string `json:"-"` // authentication token - DO NOT send this when marshalling

	// OAuth 2.0 service app authentication, mutually exclusive with Token
	Client_ID      string
	Private_Key    string `json:"-"` // Okta JWK JSON or RSA PEM - DO NOT send this when marshalling
	Private_Key_ID string // optional, overrides the JWK kid

	parsed *parsedKey // Private_Key as parsed by Verify, so the key is only parsed once
}

type parsedKey struct {
	raw string // the Private_Key it was parsed from
	key *rsa.PrivateKey
	kid string
}

// privateKey parses Private_Key, or returns what an earlier call parsed from the same value
func (c *Config) privateKey() (*rsa.PrivateKey, string, error) {
	if pk := c.parsed; pk != nil && pk.raw == c.Private_Key {
		return pk.key, pk.kid, nil
	}
	key, kid, err := parsePrivateKey(c.Private_Key)
	if err != nil {
		return nil, ``, fmt.Errorf("invalid Private-Key: %w", err)
	}
	c.parsed = &parsedKey{raw: c.Private_Key, key: key, kid: kid}
	return key, kid, nil
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
		c.Request_Batch_Size == nc.Request_Batch_Size &&
		c.Request_Per_Minute == nc.Request_Per_Minute &&
		c.Request_Burst == nc.Request_Burst &&
		c.Domain == nc.Domain &&
		c.Token == nc.Token &&
		c.Client_ID == nc.Client_ID &&
		c.Private_Key == nc.Private_Key &&
		c.Private_Key_ID == nc.Private_Key_ID
}

// useOAuth reports whether the config authenticates as an OAuth 2.0 service app rather than with an SSWS token
func (c *Config) useOAuth() bool {
	return c.Token == `` && c.Client_ID != ``
}

func (c *Config) Verify() (err error) {
	c.ApplyDefaultIngesterUUID(defaultIngesterUUIDStr)

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

	if err = c.verifyAuth(); err != nil {
		return
	}
	if c.Domain == `` {
		return errors.New("missing okta domain")
	} else if !isOktaDomain(c.Domain) {
		err = fmt.Errorf("%q is not an okta domain", c.Domain)
		return
	}

	if err := c.BaseConfig.Verify(); err != nil {
		return err
	}

	return
}

// verifyAuth enforces that exactly one of {Token} or {Client-ID, Private-Key, Private-Key-ID} is configured
func (c *Config) verifyAuth() error {
	oauth := c.Client_ID != `` || c.Private_Key != `` || c.Private_Key_ID != ``
	switch {
	case c.Token != `` && oauth:
		return errors.New("Token and Client-ID/Private-Key/Private-Key-ID are mutually exclusive")
	case c.Token != ``:
		return nil
	case !oauth:
		return errors.New("missing okta authentication: set Token, or Client-ID + Private-Key")
	case c.Client_ID == ``:
		return errors.New("Private-Key and Private-Key-ID require Client-ID")
	case c.Private_Key == ``:
		return errors.New("Client-ID requires Private-Key")
	}
	_, _, err := c.privateKey()
	return err
}

// isOktaDomain reports whether d is an org host under one of the Okta parent domains.
// Matching on a leading dot rejects look-alike hosts such as example-notokta.com.
func isOktaDomain(d string) bool {
	d = strings.ToLower(d)
	for _, sfx := range oktaDomainSuffixes {
		if strings.HasSuffix(d, sfx) {
			return true
		}
	}
	return false
}
