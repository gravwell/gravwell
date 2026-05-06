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
	"net/http"
	"net/url"
	"path"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	defaultOtelLogsURL = `/v1/logs`
)

type otelLogsListener struct {
	URL               string
	Tag_Name          string
	Ignore_Timestamps bool
	Debug_Posts       bool
	Encode_As_JSON    bool
	Disable_EVs       bool
	Preprocessor      []string
}

func (o *otelLogsListener) validate(name string) (string, error) {
	if len(o.URL) == 0 {
		o.URL = defaultOtelLogsURL
	}
	p, err := url.Parse(o.URL)
	if err != nil {
		return ``, fmt.Errorf("URL structure is invalid: %v", err)
	}
	if p.Scheme != `` {
		return ``, errors.New("May not specify scheme in listening URL")
	} else if p.Host != `` {
		return ``, errors.New("May not specify host in listening URL")
	}
	pth := path.Clean(p.Path)
	if len(o.Tag_Name) == 0 {
		o.Tag_Name = entry.DefaultTagName
	}
	if ingest.CheckTag(o.Tag_Name) != nil {
		return ``, errors.New("Invalid characters in the \"" + o.Tag_Name + "\"Tag-Name for " + name)
	}
	o.URL = pth
	return pth, nil
}

func (o *otelLogsListener) tags() ([]string, error) {
	if len(o.Tag_Name) == 0 {
		return nil, errors.New("No tags specified")
	}
	return []string{o.Tag_Name}, nil
}

func includeOtelLogsListeners(hnd *handler, igst *ingest.IngestMuxer, cfg *cfgType) (err error) {
	for k, v := range cfg.OtelLogsListener {
		oh := &otelLogsHandler{
			name:         k,
			lgr:          hnd.lgr,
			encodeAsJSON: v.Encode_As_JSON,
			disableEVs:   v.Disable_EVs,
		}
		if oh.timeWindow, err = cfg.GlobalTimestampWindow(); err != nil {
			return fmt.Errorf("TimestampWindow is invalid %w", err)
		}

		hcfg := routeHandler{
			handler:    oh.handle,
			debugPosts: v.Debug_Posts,
		}

		if hcfg.tag, err = igst.NegotiateTag(v.Tag_Name); err != nil {
			return fmt.Errorf("failed to negotiate tag %s %w", v.Tag_Name, err)
		}

		if v.Ignore_Timestamps {
			hcfg.ignoreTs = true
		} else {
			var window timegrinder.TimestampWindow
			window, err = cfg.GlobalTimestampWindow()
			if err != nil {
				return fmt.Errorf("Failed to get global timestamp window %w", err)
			}
			if hcfg.tg, err = timegrinder.New(timegrinder.Config{TSWindow: window}); err != nil {
				return fmt.Errorf("Failed to create timegrinder %w", err)
			} else if err = cfg.TimeFormat.LoadFormats(hcfg.tg); err != nil {
				return fmt.Errorf("failed to load custom time formats %w", err)
			}
		}

		if hcfg.pproc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			return fmt.Errorf("preprocessor construction error %w", err)
		}

		if err = hnd.addHandler(http.MethodPost, v.URL, hcfg); err != nil {
			return fmt.Errorf("failed to add OpenTelemetry logs handler %w", err)
		}
		debugout("Added OpenTelemetry logs listener %s %s\n", k, v.URL)
	}
	return nil
}
