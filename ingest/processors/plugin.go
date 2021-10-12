// +build !386,!arm,!mips,!mipsle,!s390x

/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors/plugin"
	"github.com/open2b/scriggo"
)

const (
	PluginProcessor     string = `plugin`
	PluginEngineScriggo string = `scriggo`

	defaultEngine     string = PluginEngineScriggo
	maxPluginFileSize int64  = 1024 * 1024 * 32 //32MB is crazy and useful in case we want to allow static binary plugins
	registerTimeout          = time.Second
)

var (
	ErrNoPlugins     = errors.New("No plugins provided in Plugin-Path")
	ErrDuplicateFile = errors.New("dupclicate plugin file")
)

// PluginData implements the fs.FS interface
type PluginData struct {
	scriggo.Files
}

type PluginConfig struct {
	Plugin_Path   []string               //path to the plugin files (this may support multifile plugins later
	Plugin_Engine string                 // defaults to scriggo
	vc            *config.VariableConfig // we keep a handle on the variable to config to pass to the underlying plugin script
	pd            PluginData
	// all other config items are dynamic and passed to the underlying plugin
}

func PluginLoadConfig(vc *config.VariableConfig) (pc PluginConfig, err error) {
	if err = vc.MapTo(&pc); err == nil {
		if err = pc.validate(); err == nil {
			pc.vc = vc //grab a handle on the variable config
		}
	}
	return
}

// validate ONLY validates the path and enging
func (pc *PluginConfig) validate() (err error) {
	//check the engine
	pc.Plugin_Engine = strings.ToLower(strings.TrimSpace(pc.Plugin_Engine))
	switch pc.Plugin_Engine {
	case ``: //deafult
		pc.Plugin_Engine = PluginEngineScriggo
	case PluginEngineScriggo: //this is fine
	default:
		err = fmt.Errorf("Unknown plugin engine %q", pc.Plugin_Engine)
		return
	}

	//check the plugin path (make sure it exists and we can read it)
	if len(pc.Plugin_Path) == 0 {
		err = ErrNoPlugins
		return
	}

	if pc.pd.count() == 0 {
		for _, p := range pc.Plugin_Path {
			if err = pc.pd.add(p); err != nil {
				break
			}
		}
	}
	return

}

type Plugin struct {
	PluginConfig
	pp *plugin.PluginProgram
}

func NewPluginProcessor(cfg PluginConfig, tg Tagger) (p *Plugin, err error) {
	if err = cfg.validate(); err == nil {
		var pp *plugin.PluginProgram
		if pp, err = plugin.NewPlugin(cfg.pd); err == nil {
			if err = pp.Run(registerTimeout); err == nil {
				if err = pp.Config(cfg.vc, tg); err == nil {
					p = &Plugin{
						PluginConfig: cfg,
						pp:           pp,
					}
				}
			}
			if err != nil {
				pp.Close()
			}
		}
	}
	return
}

func (p *Plugin) Close() (err error) {
	if p == nil || p.pp == nil {
		err = ErrNotReady
	} else {
		err = p.pp.Close()
	}
	return
}

func (p *Plugin) Flush() []*entry.Entry {
	if p == nil || p.pp == nil {
		return nil
	}
	return p.pp.Flush()
}

func (p *Plugin) Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	if p == nil || p.pp == nil {
		return nil, ErrNotReady
	}
	return p.pp.Process(ents)

}

func (pd PluginData) count() int {
	return len(pd.Files)
}

func (pd *PluginData) add(pth string) (err error) {
	var fin *os.File
	var fi os.FileInfo
	cleanPath := filepath.Clean(pth)
	if fin, err = os.Open(cleanPath); err != nil {
		return
	}
	defer fin.Close()

	if fi, err = fin.Stat(); err != nil {
		return
	}
	if fi.Size() > maxPluginFileSize {
		err = fmt.Errorf("Plugin file size is too large: %d > %d", fi.Size(), maxPluginFileSize)
		return
	}
	if pd.Files == nil {
		pd.Files = scriggo.Files{}
	}

	//get the base filename
	b := filepath.Base(pth)
	if _, ok := pd.Files[b]; ok {
		err = ErrDuplicateFile
		return
	}

	//read the file up to the max size
	bb := bytes.NewBuffer(nil)
	if _, err = io.Copy(bb, fin); err != nil {
		return
	}
	pd.Files[b] = bb.Bytes()
	return
}
