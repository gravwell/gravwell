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
)

var (
	ErrInvalidParameter = errors.New("invalid parameter")
)

// loadConfig is used during the initial configuration load at startup, the individual include calls perform
// locking on the main handler as needed, do NOT lock the handler at the top level of this function, it will just deadlock
func (h *handler) loadConfig(cfg *cfgType) (err error) {
	if cfg == nil {
		return ErrInvalidParameter
	} else if err = cfg.Verify(); err != nil {
		return
	}
	if hcurl, ok := cfg.HealthCheck(); ok {
		h.Lock()
		h.healthCheckURL = path.Clean(hcurl)
		h.Unlock()
	}

	if err = includeStdListeners(h, h.igst, cfg); err != nil {
		err = fmt.Errorf("failed to include std listeners %w", err)
	} else if err = includeHecListeners(h, h.igst, cfg); err != nil {
		err = fmt.Errorf("failed to include HEC Listeners %w", err)
	} else if err = includeAFHListeners(h, h.igst, cfg, h.lgr); err != nil {
		err = fmt.Errorf("failed to include Amazon Firehose Listeners %w", err)
	}
	return
}

func (h *handler) hotReload(cfg *cfgType) (err error) {
	if cfg == nil {
		return ErrInvalidParameter
	} else if err = cfg.Verify(); err != nil {
		return
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

	if err = includeStdListeners(tempHandler, h.igst, cfg); err != nil {
		err = fmt.Errorf("failed to include std listeners %w", err)
		return
	} else if err = includeHecListeners(tempHandler, h.igst, cfg); err != nil {
		err = fmt.Errorf("failed to include HEC Listeners %w", err)
		return
	} else if err = includeAFHListeners(tempHandler, h.igst, cfg, h.lgr); err != nil {
		err = fmt.Errorf("failed to include Amazon Firehose Listeners %w", err)
		return
	}

	// we got a good reload, lock and swap
	h.Lock()
	h.mp = tempHandler.mp
	h.auth = tempHandler.auth
	h.custom = tempHandler.custom
	h.Unlock()

	return
}
