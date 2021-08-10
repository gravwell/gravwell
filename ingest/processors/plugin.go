/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/processors/plugins"
)

type pluginServer struct {
	*plugins.IngesterProcessorPlugin
}

func NewPluginProcessor(cfg plugins.PluginConfig) (p *pluginServer, err error) {
	var ipp *plugins.IngesterProcessorPlugin
	if ipp, err = plugins.NewIngesterProcessorPlugin(cfg); err != nil {
		return
	} else if err = ipp.LoadConfig(cfg.Variables); err != nil {
		ipp.Close()
		return
	}
	p = &pluginServer{
		IngesterProcessorPlugin: ipp,
	}
	return
}

func PluginLoadConfig(vc *config.VariableConfig) (plugins.PluginConfig, error) {
	return plugins.PluginLoadConfig(vc)
}
