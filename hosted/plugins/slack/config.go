/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package slack implements a hosted poller for the Slack Audit Logs API.
package slack

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
)

const (
	Name    = "slack"
	ID      = "slack.ingesters.gravwell.io"
	Version = "1.0.0"

	defaultIngesterUUIDStr   = "17d0aef9-a8e6-4ae6-b443-6f1b68c29d0a"
	defaultHost              = "https://api.slack.com"
	defaultLookback          = 168 // hours (7 days) of initial history
	defaultRequestsPerMinute = 30
	defaultRequestInterval   = 300 // seconds
	defaultTagPrefix         = "slack"

	maximumRequestsPerMinute = 10000
	minimumRequestInterval   = 60
	maximumTokenFileSize     = 64 << 10
)

// Config is the public Slack Audit hosted-plugin configuration. Slack Audit
// currently exposes one supported dataset, so the endpoint, page size, and
// cursor mechanics are deliberately internal rather than public knobs.
type Config struct {
	hosted.BaseConfig
	hosted.MultiTagConfig
	hosted.PollingConfig

	Host           string
	API_Token      string `json:"-"`
	API_Token_File string `json:"-"`
}

var _ hosted.Config = (*Config)(nil)
var _ hosted.ConfigSanitizer = (*Config)(nil)

func (c *Config) Equal(other any) bool {
	nc, ok := hosted.EqualTarget[Config](other)
	return c != nil && ok &&
		c.BaseConfig == nc.BaseConfig &&
		c.MultiTagConfig == nc.MultiTagConfig &&
		c.PollingConfig == nc.PollingConfig &&
		c.Host == nc.Host &&
		c.API_Token == nc.API_Token &&
		c.API_Token_File == nc.API_Token_File
}

func (c *Config) Verify() error {
	if c == nil {
		return errors.New("nil Slack config")
	}

	c.ApplyDefaultIngesterUUID(defaultIngesterUUIDStr)
	if err := c.BaseConfig.Verify(); err != nil {
		return err
	}
	if err := c.MultiTagConfig.ValidateTags(); err != nil {
		return err
	}

	c.ApplyDefaults(defaultLookback, defaultRequestsPerMinute, defaultRequestInterval)
	if c.Lookback < 0 {
		return errors.New("Lookback must not be negative")
	}
	if int64(c.Lookback) > int64(math.MaxInt64)/int64(time.Hour) {
		return errors.New("Lookback exceeds the maximum safe duration")
	}
	if c.Requests_Per_Minute < 1 || c.Requests_Per_Minute > maximumRequestsPerMinute {
		return fmt.Errorf("Requests-Per-Minute must be between 1 and %d", maximumRequestsPerMinute)
	}
	if c.Request_Interval < minimumRequestInterval {
		return fmt.Errorf("Request-Interval must be at least %d seconds", minimumRequestInterval)
	}
	if int64(c.Request_Interval) > int64(math.MaxInt64)/int64(time.Second) {
		return errors.New("Request-Interval exceeds the maximum safe duration")
	}
	if _, err := c.credential(); err != nil {
		return err
	}

	if c.Host == "" {
		c.Host = defaultHost
	}
	host, err := validateHost(c.Host)
	if err != nil {
		return err
	}
	c.Host = host
	return nil
}

func (c *Config) credential() (string, error) {
	if c == nil {
		return "", errors.New("nil Slack config")
	}
	if c.API_Token_File != "" {
		return readTokenFile(c.API_Token_File)
	}
	return validateToken(c.API_Token, "API-Token")
}

func readTokenFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("API-Token-File: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("API-Token-File must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("API-Token-File must not be accessible by group or other users")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("API-Token-File: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("API-Token-File: %w", err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return "", errors.New("API-Token-File changed while it was being opened")
	}
	if openedInfo.Mode().Perm()&0o077 != 0 {
		return "", errors.New("API-Token-File must not be accessible by group or other users")
	}
	body, err := io.ReadAll(io.LimitReader(file, maximumTokenFileSize+1))
	if err != nil {
		return "", fmt.Errorf("API-Token-File: %w", err)
	}
	if len(body) > maximumTokenFileSize {
		return "", fmt.Errorf("API-Token-File exceeds %d bytes", maximumTokenFileSize)
	}
	return validateToken(string(body), "API-Token-File")
}

func validateToken(raw, field string) (string, error) {
	value := strings.TrimSuffix(raw, "\n")
	value = strings.TrimSuffix(value, "\r")
	if value == "" {
		return "", fmt.Errorf("%s not specified", field)
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("%s must contain exactly one non-empty line", field)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s not specified", field)
	}
	return value, nil
}

func validateHost(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("Host must be an absolute URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Host must not contain userinfo, query parameters, or a fragment")
	}
	if u.Path != "" && u.Path != "/" {
		return "", errors.New("Host must not contain a path")
	}
	if u.Scheme != "https" {
		hostname := u.Hostname()
		ip := net.ParseIP(hostname)
		if u.Scheme != "http" || !(strings.EqualFold(hostname, "localhost") || ip != nil && ip.IsLoopback()) {
			return "", errors.New("Host must use HTTPS; HTTP is allowed only for loopback tests")
		}
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func (c *Config) tag() string {
	return c.ResolveTag("audit", defaultTagPrefix)
}

func (c *Config) Tags() []string {
	return []string{c.tag()}
}

// SanitizedConfig opts into child-state reporting without exposing Token.
func (c *Config) SanitizedConfig() any {
	if c == nil {
		return nil
	}
	return struct {
		Ingester_UUID       string
		Tag_Name            string
		Tag_Prefix          string
		Lookback            int
		Requests_Per_Minute int
		Request_Interval    int
		Host                string
	}{
		Ingester_UUID:       c.Ingester_UUID,
		Tag_Name:            c.Tag_Name,
		Tag_Prefix:          c.Tag_Prefix,
		Lookback:            c.Lookback,
		Requests_Per_Minute: c.Requests_Per_Minute,
		Request_Interval:    c.Request_Interval,
		Host:                c.Host,
	}
}
