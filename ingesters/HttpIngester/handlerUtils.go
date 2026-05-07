/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
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
	"path"

	"github.com/gravwell/gravwell/v4/ingest/log"
)

var (
	ErrInvalidParameter = errors.New("invalid parameter")
)

// loadConfig is used during the initial configuration load at startup, the individual include calls perform
// locking on the main handler as needed, do NOT lock the handler at the top level of this function, it will just deadlock
func (h *handler) loadConfig(cfg *cfgType) error {
	if cfg == nil {
		return ErrInvalidParameter
	}

	if err := cfg.Verify(); err != nil {
		return err
	}

	if hcurl, ok := cfg.HealthCheck(); ok {
		h.Lock()
		h.healthCheckURL = path.Clean(hcurl)
		h.Unlock()
	}

	if err := includeStdListeners(h, h.igst, cfg); err != nil {
		return fmt.Errorf("failed to include std listeners %w", err)
	}
	if err := includeHecListeners(h, h.igst, cfg); err != nil {
		return fmt.Errorf("failed to include HEC Listeners %w", err)
	}
	if err := includeAFHListeners(h, h.igst, cfg, h.lgr); err != nil {
		return fmt.Errorf("failed to include Amazon Firehose Listeners %w", err)
	}
	if err := h.igst.SetRawConfiguration(cfg); err != nil {
		return fmt.Errorf("failed to set raw configuration %w", err)
	}

	return nil
}

func (h *handler) hotReload(cfg *cfgType) error {
	if cfg == nil {
		return ErrInvalidParameter
	}

	if err := cfg.Verify(); err != nil {
		return err
	}

	//check healthCheck URL and load it if set
	h.Lock()
	if hcurl, ok := cfg.HealthCheck(); ok {
		h.healthCheckURL = path.Clean(hcurl)
	} else {
		h.healthCheckURL = ``
	}
	h.Unlock()

	//make a fake handler so that we can re=use the maps and just do a hard swap
	tempHandler := &handler{
		igst:   h.igst, // may not be needed but no harm either
		lgr:    h.lgr,  // may not be needed but no harm either
		mp:     make(map[route]routeHandler),
		auth:   make(map[route]authHandler),
		custom: make(map[route]http.Handler),
	}

	if err := includeStdListeners(tempHandler, h.igst, cfg); err != nil {
		return fmt.Errorf("failed to include std listeners %w", err)
	}
	if err := includeHecListeners(tempHandler, h.igst, cfg); err != nil {
		return fmt.Errorf("failed to include HEC Listeners %w", err)
	}
	if err := includeAFHListeners(tempHandler, h.igst, cfg, h.lgr); err != nil {
		return fmt.Errorf("failed to include Amazon Firehose Listeners %w", err)
	}

	// we got a good reload, lock and swap
	h.Lock()
	h.mp = tempHandler.mp
	h.auth = tempHandler.auth
	h.custom = tempHandler.custom
	h.Unlock()

	if err := h.igst.SetRawConfiguration(cfg); err != nil {
		// At this point, the reload is already committed.
		// Let's not signal a failed reload and just log the error instead of returning it.
		_ = h.lgr.Error("error setting raw configuration during hot reload", log.KVErr(err))
	}

	tags, err := cfg.Tags()
	if err != nil {
		// Tag negotiation during hot reload shouldn't stop the whole thing, so just log the error.
		_ = h.lgr.Error("error getting tags from config", log.KVErr(err))
	} else {
		for _, t := range tags {
			if _, err := h.igst.NegotiateTag(t); err != nil {
				// Tag negotiation during hot reload shouldn't stop the whole thing, so just log the error.
				_ = h.lgr.Error("error negotiating tag", log.KVErr(err), log.KV("tag", t))
			}
		}
	}

	return nil
}
