/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package wiz

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/ingest"
)

const (
	defaultIngesterUUIDStr string = "e83e63d2-382e-4b42-b2da-77e2708143f1"
	// defaultAuthURL is the Wiz OAuth token endpoint used to exchange a client
	// id/secret for a bearer token. Override for gov/fedramp tenants.
	defaultAuthURL = "https://auth.app.wiz.io/oauth/token"
	// defaultAudience is the OAuth audience Wiz expects for API tokens.
	defaultAudience = "wiz-api"

	// wizAPIDomain is the domain suffix a GraphQL endpoint must live under.
	wizAPIDomain = "app.wiz.io"
	// wizAuthDomain is the domain suffix the auth endpoint must live under.
	wizAuthDomain = "wiz.io"

	defaultLookback          = 24  // hours of history to pull on the first scan
	defaultRequestsPerMinute = 60  // Wiz default API rate limit
	defaultInterval          = 300 // seconds between poll cycles
	defaultPageSize          = 100 // GraphQL connection page size
	defaultMaxPagesPerType   = 20  // pages consumed per event type per poll cycle
)

type Config struct {
	hosted.BaseConfig
	hosted.PollingConfig
	Client_Id          string   `json:"-"` // DO NOT send this when marshalling
	Client_Secret      string   `json:"-"` // DO NOT send this when marshalling
	Endpoint           string   // GraphQL API endpoint, e.g. https://api.us1.app.wiz.io/graphql
	Auth_URL           string   // optional override for the OAuth token endpoint
	Audience           string   // optional override for the OAuth audience
	Page_Size          int      // number of nodes requested per GraphQL page
	Max_Pages_Per_Type int      // pages drained per event type per poll cycle
	Tag_Name           string   // required; all events land here unless overridden
	Tag_Override       []string // optional per-source routing, "source:tag"
	Query_Override     []string // optional per-source query file, "source:/path/to/query.graphql"

	tags    map[string]string // parsed Tag_Override
	queries map[string]string // parsed Query_Override, source -> query document
}

var _ hosted.Config = (*Config)(nil) // compile time interface check

// Equal implements hosted.Config so the runner can decide whether a config reload
// actually changed anything for this ingester.
// The parsed tags and queries are compared as well, that catches an edited query
// override file, whose contents change without the config file itself changing.
func (c *Config) Equal(ncp any) bool {
	nc, ok := hosted.EqualTarget[Config](ncp)
	if c == nil || !ok {
		return false
	}
	return c.BaseConfig == nc.BaseConfig &&
		c.PollingConfig == nc.PollingConfig &&
		c.Client_Id == nc.Client_Id &&
		c.Client_Secret == nc.Client_Secret &&
		c.Endpoint == nc.Endpoint &&
		c.Auth_URL == nc.Auth_URL &&
		c.Audience == nc.Audience &&
		c.Page_Size == nc.Page_Size &&
		c.Max_Pages_Per_Type == nc.Max_Pages_Per_Type &&
		c.Tag_Name == nc.Tag_Name &&
		slices.Equal(c.Tag_Override, nc.Tag_Override) &&
		slices.Equal(c.Query_Override, nc.Query_Override) &&
		maps.Equal(c.tags, nc.tags) &&
		maps.Equal(c.queries, nc.queries)
}

func (c *Config) Verify() error {
	c.ApplyDefaultIngesterUUID(defaultIngesterUUIDStr)

	if c.Client_Id == "" {
		return errors.New("Client-Id not specified")
	}
	if c.Client_Secret == "" {
		return errors.New("Client-Secret not specified")
	}

	if err := c.verifyEndpoint(); err != nil {
		return err
	}

	if c.Auth_URL == "" {
		c.Auth_URL = defaultAuthURL
	} else if err := verifyAuthURL(c.Auth_URL); err != nil {
		return err
	}

	if c.Audience == "" {
		c.Audience = defaultAudience
	}

	c.PollingConfig.ApplyDefaults(defaultLookback, defaultRequestsPerMinute, defaultInterval)
	if c.Page_Size <= 0 {
		c.Page_Size = defaultPageSize
	}
	if c.Max_Pages_Per_Type <= 0 {
		c.Max_Pages_Per_Type = defaultMaxPagesPerType
	}

	if c.Tag_Name == "" {
		return errors.New("Tag-Name not specified")
	}
	if err := ingest.CheckTag(c.Tag_Name); err != nil {
		return fmt.Errorf("invalid Tag-Name %q: %w", c.Tag_Name, err)
	}
	if err := c.parseTagOverrides(); err != nil {
		return err
	}
	if err := c.parseQueryOverrides(); err != nil {
		return err
	}
	return nil
}

func (c *Config) verifyEndpoint() error {
	if c.Endpoint == "" {
		return errors.New("Endpoint not specified")
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil {
		return fmt.Errorf("invalid Endpoint %q: %w", c.Endpoint, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("Endpoint %q must use https", c.Endpoint)
	}
	if u.Host == "" {
		return fmt.Errorf("Endpoint %q is missing a host", c.Endpoint)
	}
	if host := u.Hostname(); host != wizAPIDomain && !strings.HasSuffix(host, "."+wizAPIDomain) {
		return fmt.Errorf("Endpoint host %q is not a %s GraphQL endpoint", host, wizAPIDomain)
	}
	return nil
}

func verifyAuthURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid Auth-URL %q: %w", raw, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("Auth-URL %q must use https", raw)
	}
	if host := u.Hostname(); host != wizAuthDomain && !strings.HasSuffix(host, "."+wizAuthDomain) {
		return fmt.Errorf("Auth-URL host %q is not a %s host", host, wizAuthDomain)
	}
	return nil
}

// parseTagOverrides parses "source:tag" entries into a lookup keyed by source.
func (c *Config) parseTagOverrides() error {
	c.tags = make(map[string]string, len(c.Tag_Override))
	for _, entry := range c.Tag_Override {
		source, tag, ok := cutSourceValue(entry)
		if !ok {
			return fmt.Errorf("invalid Tag-Override %q, expected \"source:tag\"", entry)
		}
		if !knownSources[source] {
			return fmt.Errorf("Tag-Override references unknown source %q", source)
		}
		if err := ingest.CheckTag(tag); err != nil {
			return fmt.Errorf("invalid Tag-Override tag %q: %w", tag, err)
		}
		c.tags[source] = tag
	}
	return nil
}

// parseQueryOverrides loads "source:/path/to/query.graphql" entries, reading the
// replacement query document for the named source from disk.
func (c *Config) parseQueryOverrides() error {
	c.queries = make(map[string]string, len(c.Query_Override))
	for _, entry := range c.Query_Override {
		source, path, ok := cutSourceValue(entry)
		if !ok {
			return fmt.Errorf("invalid Query-Override %q, expected \"source:/path/to/query.graphql\"", entry)
		}
		if !knownSources[source] {
			return fmt.Errorf("Query-Override references unknown source %q", source)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read Query-Override file for %q: %w", source, err)
		}
		doc := strings.TrimSpace(string(b))
		if doc == "" {
			return fmt.Errorf("Query-Override file for %q is empty", source)
		}
		c.queries[source] = doc
	}
	return nil
}

// cutSourceValue splits a "source:value" config entry, trimming whitespace.
func cutSourceValue(entry string) (source, value string, ok bool) {
	source, value, ok = strings.Cut(entry, ":")
	source = strings.TrimSpace(source)
	value = strings.TrimSpace(value)
	if !ok || source == "" || value == "" {
		return "", "", false
	}
	return source, value, true
}

// Tags returns every tag the plugin may write to: the default Tag-Name plus any
// per-source overrides, de-duplicated, so the runtime can pre-register them.
func (c *Config) Tags() []string {
	tags := make([]string, 0, len(c.tags)+1)
	seen := make(map[string]bool, len(c.tags)+1)
	add := func(tag string) {
		if tag == "" || seen[tag] {
			return
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	add(c.Tag_Name)
	for _, tag := range c.tags {
		add(tag)
	}
	return tags
}

// tagFor resolves the tag for a given source, honoring any override and falling
// back to the default Tag-Name.
func (c *Config) tagFor(sourceName string) string {
	if tag, ok := c.tags[sourceName]; ok {
		return tag
	}
	return c.Tag_Name
}
