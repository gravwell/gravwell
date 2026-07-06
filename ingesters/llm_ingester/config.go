/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol"
)

const (
	defaultMaxBody    = 16 * 1024 * 1024 // 16 MiB
	defaultSessionTTL = 30 * time.Minute
	defaultUpstream   = "https://api.openai.com"

	logModeDeltas           = "deltas"
	logModeUserOnly         = "user-only"
	logModeFullConversation = "full-conversation"
)

type gbl struct {
	config.IngestConfig
	State_Store_Location string
}

type cfgReadType struct {
	Global       gbl
	Attach       attach.AttachConfig
	Listener     map[string]*listener
	Preprocessor processors.ProcessorConfig
}

type listener struct {
	Bind                 string
	Upstream_URL         string
	Protocol             string
	Tag_Name             string
	Log_Mode             string
	Log_Tool_Calls       bool
	Log_Usage            bool
	Redact_Authorization bool
	Session_TTL          string
	Max_Body             int
	TLS_Certificate_File string
	TLS_Key_File         string
	Insecure_Skip_TLS_Verify_Upstream bool
	Preprocessor         []string

	// derived during Verify
	sessionTTL  time.Duration
	upstreamURL *url.URL
}

type cfgType struct {
	gbl
	Attach       attach.AttachConfig
	Listener     map[string]*listener
	Preprocessor processors.ProcessorConfig
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	var cr cfgReadType
	if err := config.LoadConfigFile(&cr, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&cr, overlayPath); err != nil {
		return nil, err
	}
	c := &cfgType{
		gbl:          cr.Global,
		Attach:       cr.Attach,
		Listener:     cr.Listener,
		Preprocessor: cr.Preprocessor,
	}
	if err := c.Verify(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *cfgType) Verify() error {
	if err := c.IngestConfig.Verify(); err != nil {
		return err
	}
	if err := c.Attach.Verify(); err != nil {
		return err
	}
	if len(c.Listener) == 0 {
		return errors.New("no listeners defined")
	}
	if err := c.Preprocessor.Validate(); err != nil {
		return err
	}
	binds := map[string]string{}
	for name, l := range c.Listener {
		if err := l.validate(name); err != nil {
			return fmt.Errorf("listener %q: %w", name, err)
		}
		if other, ok := binds[l.Bind]; ok {
			return fmt.Errorf("listener %q bind %q duplicated (also used by %q)", name, l.Bind, other)
		}
		binds[l.Bind] = name
		if err := c.Preprocessor.CheckProcessors(l.Preprocessor); err != nil {
			return fmt.Errorf("listener %q preprocessor invalid: %w", name, err)
		}
	}
	return nil
}

func (l *listener) validate(name string) error {
	if l.Bind == "" {
		return errors.New("missing Bind")
	}
	if l.Upstream_URL == "" {
		l.Upstream_URL = defaultUpstream
	}
	u, err := url.Parse(l.Upstream_URL)
	if err != nil {
		return fmt.Errorf("invalid Upstream-URL: %w", err)
	} else if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("Upstream-URL scheme must be http or https, got %q", u.Scheme)
	} else if u.Host == "" {
		return errors.New("Upstream-URL must include a host")
	}
	l.upstreamURL = u
	if l.Protocol == "" {
		return errors.New("missing Protocol")
	}
	if _, err := protocol.Lookup(l.Protocol); err != nil {
		return err
	}
	if l.Tag_Name == "" {
		l.Tag_Name = entry.DefaultTagName
	}
	if err := ingest.CheckTag(l.Tag_Name); err != nil {
		return fmt.Errorf("invalid Tag-Name %q: %w", l.Tag_Name, err)
	}
	switch l.Log_Mode {
	case "":
		l.Log_Mode = logModeDeltas
	case logModeDeltas, logModeUserOnly, logModeFullConversation:
	default:
		return fmt.Errorf("invalid Log-Mode %q (want %q, %q, or %q)",
			l.Log_Mode, logModeDeltas, logModeUserOnly, logModeFullConversation)
	}
	if l.Max_Body <= 0 {
		l.Max_Body = defaultMaxBody
	}
	if l.Session_TTL == "" {
		l.sessionTTL = defaultSessionTTL
	} else {
		d, err := time.ParseDuration(l.Session_TTL)
		if err != nil {
			return fmt.Errorf("invalid Session-TTL: %w", err)
		} else if d <= 0 {
			return errors.New("Session-TTL must be positive")
		}
		l.sessionTTL = d
	}
	if l.TLS_Certificate_File != "" || l.TLS_Key_File != "" {
		if l.TLS_Certificate_File == "" || l.TLS_Key_File == "" {
			return errors.New("both TLS-Certificate-File and TLS-Key-File must be set")
		}
		if _, err := tls.LoadX509KeyPair(l.TLS_Certificate_File, l.TLS_Key_File); err != nil {
			return fmt.Errorf("TLS keypair: %w", err)
		}
	}
	return nil
}

func (l *listener) TLSEnabled() bool {
	return l.TLS_Certificate_File != "" && l.TLS_Key_File != ""
}

func (l *listener) UpstreamURL() *url.URL {
	return l.upstreamURL
}

func (l *listener) SessionTTL() time.Duration {
	return l.sessionTTL
}

// Tags returns the list of tags used across all listeners.
func (c *cfgType) Tags() ([]string, error) {
	seen := map[string]bool{}
	var tags []string
	for _, l := range c.Listener {
		if l.Tag_Name == "" || seen[l.Tag_Name] {
			continue
		}
		seen[l.Tag_Name] = true
		tags = append(tags, l.Tag_Name)
	}
	if len(tags) == 0 {
		return nil, errors.New("no tags configured")
	}
	sort.Strings(tags)
	return tags, nil
}

func (c *cfgType) IngestBaseConfig() config.IngestConfig {
	return c.IngestConfig
}

func (c *cfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
}
