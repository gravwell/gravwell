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

	"github.com/gravwell/gravwell/v4/hosted"
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
	defaultUserPollInterval = 10 * time.Minute
	minUserPollInterval     = 5 * time.Minute // every poll lists every user, do not let it run hotter than this
	defaultUserDumpInterval = 24 * time.Hour
	minUserDumpInterval     = 10 * time.Minute // and it must also be longer than the poll interval
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
	Token              string `json:"-" dynamic:"secret,requiredif=Client-ID:"` // authentication token - DO NOT send this when marshalling
	User_Poll_Interval string // how often users are listed and the changed ones emitted; parsed by time.ParseDuration, default 10m, min 5m
	User_Dump_Interval string // how often every user is emitted, changed or not; parsed by time.ParseDuration, default 24h

	// OAuth 2.0 service app authentication, mutually exclusive with Token
	Client_ID        string
	Private_Key      string `json:"-" dynamic:"secret"` // Okta JWK JSON or RSA PEM - DO NOT send this when marshalling
	Private_Key_File string `json:"-"`                  // a file holding the key, mutually exclusive with Private_Key - DO NOT send this when marshalling
	Private_Key_ID   string // optional, overrides the JWK kid

	parsed *parsedKey // the key Verify loaded from Private_Key or Private_Key_File, read and parsed once
}

type parsedKey struct {
	raw, file string // the Private_Key or Private_Key_File it came from
	key       *rsa.PrivateKey
	kid       string
}

// privateKey parses Private_Key, or reads and parses Private_Key_File. A later call for the same
// setting returns the same key, the file is read once.
func (c *Config) privateKey() (*rsa.PrivateKey, string, error) {
	if pk := c.loaded(); pk != nil {
		return pk.key, pk.kid, nil
	}
	raw, name := c.Private_Key, `Private-Key`
	if c.Private_Key_File != `` {
		b, err := readKeyFile(c.Private_Key_File)
		if err != nil {
			return nil, ``, fmt.Errorf("failed to read Private-Key-File: %w", err)
		}
		raw, name = string(b), `Private-Key-File`
	}
	key, kid, err := parsePrivateKey(raw)
	if err != nil {
		if errors.Is(err, errKeyEncoding) && c.Private_Key_File == `` {
			return nil, ``, fmt.Errorf("invalid %s: %w; a file path belongs in Private-Key-File", name, err)
		}
		return nil, ``, fmt.Errorf("invalid %s: %w", name, err)
	}
	c.parsed = &parsedKey{raw: c.Private_Key, file: c.Private_Key_File, key: key, kid: kid}
	return key, kid, nil
}

// sameKey reports whether nc loaded the same key. The keys are compared rather than the settings:
// a rotated Private-Key-File keeps its path, and a reformatted Private-Key is still the same key.
func (c *Config) sameKey(nc *Config) bool {
	a, b := c.loaded(), nc.loaded()
	if a == nil || b == nil {
		// a side whose settings have not been through Verify has no key to compare, the settings decide
		return a == nil && b == nil && c.Private_Key == nc.Private_Key
	}
	// Private_Key_ID overrides the JWK kid, and Equal has already compared it
	return (c.Private_Key_ID != `` || a.kid == b.kid) && a.key.Equal(b.key)
}

// loaded returns the key Verify loaded for the current settings, nil if there is none or the
// settings have changed since
func (c *Config) loaded() *parsedKey {
	if pk := c.parsed; pk != nil && pk.raw == c.Private_Key && pk.file == c.Private_Key_File {
		return pk
	}
	return nil
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
		c.User_Poll_Interval == nc.User_Poll_Interval &&
		c.User_Dump_Interval == nc.User_Dump_Interval &&
		c.Client_ID == nc.Client_ID &&
		c.Private_Key_File == nc.Private_Key_File &&
		c.Private_Key_ID == nc.Private_Key_ID &&
		c.sameKey(nc)
}

// userPollInterval is how often users are listed and the changed ones emitted
func (c *Config) userPollInterval() time.Duration {
	if d, err := time.ParseDuration(c.User_Poll_Interval); err == nil && d >= minUserPollInterval {
		return d
	}
	return defaultUserPollInterval
}

// userDumpInterval is how often every user is emitted, changed or not
func (c *Config) userDumpInterval() time.Duration {
	if d, err := time.ParseDuration(c.User_Dump_Interval); err == nil && d >= minUserDumpInterval {
		return d
	}
	return defaultUserDumpInterval
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

	if c.User_Poll_Interval != `` {
		d, perr := time.ParseDuration(c.User_Poll_Interval)
		if perr != nil {
			return fmt.Errorf("invalid User-Poll-Interval %q: %w", c.User_Poll_Interval, perr)
		} else if d < minUserPollInterval {
			return fmt.Errorf("User-Poll-Interval %q is less than the minimum of 5m", c.User_Poll_Interval)
		}
	}
	if c.User_Dump_Interval != `` {
		d, perr := time.ParseDuration(c.User_Dump_Interval)
		if perr != nil {
			return fmt.Errorf("invalid User-Dump-Interval %q: %w", c.User_Dump_Interval, perr)
		} else if d < minUserDumpInterval {
			return fmt.Errorf("User-Dump-Interval %q is less than the minimum of 10m", c.User_Dump_Interval)
		}
	}
	// compared as they take effect, so a long poll is checked against the default dump interval too.
	// Dumps ride on the user polls, a dump no longer than a poll would make every poll a dump.
	if poll, dump := c.userPollInterval(), c.userDumpInterval(); dump <= poll {
		return fmt.Errorf("User-Dump-Interval of %v must be longer than the User-Poll-Interval of %v", dump, poll)
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

// verifyAuth enforces that exactly one of {Token} or {Client-ID, a private key, Private-Key-ID} is
// configured, the private key being exactly one of Private-Key or Private-Key-File
func (c *Config) verifyAuth() error {
	key := c.Private_Key != `` || c.Private_Key_File != ``
	oauth := c.Client_ID != `` || key || c.Private_Key_ID != ``
	switch {
	case c.Token != `` && oauth:
		return errors.New("Token and Client-ID/Private-Key/Private-Key-File/Private-Key-ID are mutually exclusive")
	case c.Token != ``:
		return nil
	case !oauth:
		return errors.New("missing okta authentication: set Token, or Client-ID + Private-Key or Private-Key-File")
	case c.Client_ID == ``:
		return errors.New("Private-Key, Private-Key-File and Private-Key-ID require Client-ID")
	case !key:
		return errors.New("Client-ID requires Private-Key or Private-Key-File")
	case c.Private_Key != `` && c.Private_Key_File != ``:
		return errors.New("Private-Key and Private-Key-File are mutually exclusive")
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
