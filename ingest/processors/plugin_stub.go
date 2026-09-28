//go:build 386 || arm || mips || mipsle || s390x
// +build 386 arm mips mipsle s390x

/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"errors"

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

const (
	PluginProcessor string = `plugin`
)

var (
	ErrNotSupported = errors.New("plugins are not supported on 32bit architectures")
)

type PluginConfig struct{}
type Plugin struct{}

func PluginLoadConfig(vc *config.VariableConfig) (pc PluginConfig, err error) {
	err = ErrNotSupported
	return
}

func NewPluginProcessor(cfg PluginConfig, tg Tagger) (p *Plugin, err error) {
	err = ErrNotSupported
	return
}

func (p *Plugin) Close() error {
	return ErrNotSupported
}

func (p *Plugin) Flush() []*entry.Entry {
	return nil
}

func (p *Plugin) Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	return nil, ErrNotSupported
}
