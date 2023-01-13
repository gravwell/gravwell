/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"strings"

	"github.com/gravwell/gravwell/v3/ingest/config"
)

func checkProcessorOS(id string) error {
	switch id {
	case PersistentBufferProcessor:
	default:
		return ErrUnknownProcessor
	}
	return nil
}

func processorLoadConfigOS(vc *config.VariableConfig) (cfg interface{}, err error) {
	var pb preprocessorBase
	if err = vc.MapTo(&pb); err != nil {
		return
	}
	switch strings.TrimSpace(strings.ToLower(pb.Type)) {
	case PersistentBufferProcessor:
		cfg, err = PersistentBufferLoadConfig(vc)
	default:
		err = ErrUnknownProcessor
	}
	return
}

func newProcessorOS(vc *config.VariableConfig, tgr Tagger) (p Processor, err error) {
	var pb preprocessorBase
	if err = vc.MapTo(&pb); err != nil {
		return
	}
	id := strings.TrimSpace(strings.ToLower(pb.Type))
	switch id {
	case PersistentBufferProcessor:
		var cfg PersistentBufferConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewPersistentBuffer(cfg, tgr)
	default:
		err = ErrUnknownProcessor
	}
	return
}
