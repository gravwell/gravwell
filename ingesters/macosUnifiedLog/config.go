/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"sort"
	"strings"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

var (
	// values accepted by `log stream --level`
	validLevels = []string{`default`, `info`, `debug`}
	// values accepted by `log stream --type`
	validTypes = []string{`activity`, `log`, `trace`}
)

// streamConfig describes a single `log stream` process and where its events go.
// Each stanza runs its own process, so different predicates can be routed to different tags.
type streamConfig struct {
	Tag_Name          string
	Level             string   // --level: default, info, or debug
	Predicate         string   // --predicate: an NSPredicate filter expression
	Process           []string // --process: a PID or process name, may be repeated
	Type              []string // --type: activity, log, or trace, may be repeated
	Include_Source    bool     // --source: include symbol names and source line numbers
	Ignore_Timestamps bool     // stamp entries with the ingest time rather than the event timestamp
	Source_Override   string
	Preprocessor      []string

	src net.IP // parsed Source_Override
}

type cfgType struct {
	Global       config.IngestConfig
	Attach       attach.AttachConfig
	Stream       map[string]*streamConfig
	Preprocessor processors.ProcessorConfig
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	var c cfgType
	if err := config.LoadConfigFile(&c, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&c, overlayPath); err != nil {
		return nil, err
	}
	if err := c.Verify(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *cfgType) Verify() error {
	if err := c.Global.Verify(); err != nil {
		return err
	} else if err = c.Attach.Verify(); err != nil {
		return err
	} else if err = c.Preprocessor.Validate(); err != nil {
		return err
	}

	if len(c.Stream) == 0 {
		return errors.New("no Stream stanzas specified")
	}
	for k, v := range c.Stream {
		if v == nil {
			return fmt.Errorf("stream %q is empty", k)
		}
		if err := v.validate(); err != nil {
			return fmt.Errorf("stream %q: %w", k, err)
		}
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("stream %q preprocessor invalid: %w", k, err)
		}
	}
	return nil
}

func (s *streamConfig) validate() error {
	if s.Tag_Name = strings.TrimSpace(s.Tag_Name); s.Tag_Name == `` {
		s.Tag_Name = entry.DefaultTagName
	}
	if err := ingest.CheckTag(s.Tag_Name); err != nil {
		return fmt.Errorf("invalid Tag-Name %q: %w", s.Tag_Name, err)
	}

	s.Level = strings.ToLower(strings.TrimSpace(s.Level))
	if s.Level != `` && !slices.Contains(validLevels, s.Level) {
		return fmt.Errorf("invalid Level %q, must be one of %s", s.Level, strings.Join(validLevels, ", "))
	}

	for i, t := range s.Type {
		t = strings.ToLower(strings.TrimSpace(t))
		if !slices.Contains(validTypes, t) {
			return fmt.Errorf("invalid Type %q, must be one of %s", t, strings.Join(validTypes, ", "))
		}
		s.Type[i] = t
	}

	for i, p := range s.Process {
		if p = strings.TrimSpace(p); p == `` {
			return errors.New("empty Process value")
		}
		s.Process[i] = p
	}

	s.Predicate = strings.TrimSpace(s.Predicate)

	if s.Source_Override != `` {
		if s.src = net.ParseIP(s.Source_Override); s.src == nil {
			return fmt.Errorf("invalid Source-Override %q", s.Source_Override)
		}
	}
	return nil
}

// args builds the argument list handed to the `log` command for this stream.
// The arguments are passed directly to the process rather than through a shell,
// so predicates need no additional quoting.
func (s *streamConfig) args() []string {
	args := []string{`stream`, `--style`, `json`}
	if s.Level != `` {
		args = append(args, `--level`, s.Level)
	}
	if s.Predicate != `` {
		args = append(args, `--predicate`, s.Predicate)
	}
	for _, p := range s.Process {
		args = append(args, `--process`, p)
	}
	for _, t := range s.Type {
		args = append(args, `--type`, t)
	}
	if s.Include_Source {
		args = append(args, `--source`)
	}
	return args
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, len(c.Stream))
	for _, v := range c.Stream {
		if v.Tag_Name == `` || tagMp[v.Tag_Name] {
			continue
		}
		tags = append(tags, v.Tag_Name)
		tagMp[v.Tag_Name] = true
	}
	if len(tags) == 0 {
		return nil, errors.New("no tags specified")
	}
	sort.Strings(tags)
	return tags, nil
}

func (c *cfgType) IngestBaseConfig() config.IngestConfig {
	return c.Global
}

func (c *cfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
}
