/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
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

func CheckProcessor(id string) error {
	id = strings.TrimSpace(strings.ToLower(id))
	switch id {
	case GzipProcessor:
	case JsonExtractProcessor:
	case JsonArraySplitProcessor:
	case JsonFilterProcessor:
	case RegexTimestampProcessor:
	case RegexExtractProcessor:
	case RegexRouterProcessor:
	case ForwarderProcessor:
	case VpcProcessor:
	case GravwellForwarderProcessor:
	case DropProcessor:
	case CiscoISEProcessor:
	case SrcRouterProcessor:
	default:
		return ErrUnknownProcessor
	}
	return nil
}

func ProcessorLoadConfig(vc *config.VariableConfig) (cfg interface{}, err error) {
	var pb preprocessorBase
	if err = vc.MapTo(&pb); err != nil {
		return
	}
	switch strings.TrimSpace(strings.ToLower(pb.Type)) {
	case DropProcessor:
		cfg, err = DropLoadConfig(vc)
	case GzipProcessor:
		cfg, err = GzipLoadConfig(vc)
	case JsonExtractProcessor:
		cfg, err = JsonExtractLoadConfig(vc)
	case JsonArraySplitProcessor:
		cfg, err = JsonArraySplitLoadConfig(vc)
	case JsonFilterProcessor:
		cfg, err = JsonFilterLoadConfig(vc)
	case RegexTimestampProcessor:
		cfg, err = RegexTimestampLoadConfig(vc)
	case RegexExtractProcessor:
		cfg, err = RegexExtractLoadConfig(vc)
	case RegexRouterProcessor:
		cfg, err = RegexRouteLoadConfig(vc)
	case ForwarderProcessor:
		cfg, err = ForwarderLoadConfig(vc)
	case VpcProcessor:
		cfg, err = VpcLoadConfig(vc)
	case GravwellForwarderProcessor:
		cfg, err = GravwellForwarderLoadConfig(vc)
	case CiscoISEProcessor:
		cfg, err = CiscoISELoadConfig(vc)
	case SrcRouterProcessor:
		cfg, err = SrcRouteLoadConfig(vc)
	default:
		err = ErrUnknownProcessor
	}
	return
}

func newProcessor(vc *config.VariableConfig, tgr Tagger) (p Processor, err error) {
	var pb preprocessorBase
	if err = vc.MapTo(&pb); err != nil {
		return
	}
	id := strings.TrimSpace(strings.ToLower(pb.Type))
	switch id {
	case DropProcessor:
		var cfg DropConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewDrop(cfg)
	case GzipProcessor:
		var cfg GzipDecompressorConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewGzipDecompressor(cfg)
	case JsonExtractProcessor:
		var cfg JsonExtractConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewJsonExtractor(cfg)
	case JsonArraySplitProcessor:
		var cfg JsonArraySplitConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewJsonArraySplitter(cfg)
	case JsonFilterProcessor:
		var cfg JsonFilterConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewJsonFilter(cfg)
	case RegexTimestampProcessor:
		var cfg RegexTimestampConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewRegexTimestampProcessor(cfg)
	case RegexExtractProcessor:
		var cfg RegexExtractConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewRegexExtractor(cfg)
	case RegexRouterProcessor:
		var cfg RegexRouteConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewRegexRouter(cfg, tgr)
	case ForwarderProcessor:
		var cfg ForwarderConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewForwarder(cfg, tgr)
	case VpcProcessor:
		var cfg VpcConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewVpcProcessor(cfg)
	case GravwellForwarderProcessor:
		var cfg GravwellForwarderConfig
		if cfg, err = GravwellForwarderLoadConfig(vc); err != nil {
			return
		}
		p, err = NewGravwellForwarder(cfg, tgr)
	case CiscoISEProcessor:
		var cfg CiscoISEConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewCiscoISEProcessor(cfg)
	case SrcRouterProcessor:
		var cfg SrcRouteConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewSrcRouter(cfg, tgr)
	default:
		err = ErrUnknownProcessor
	}
	return
}
