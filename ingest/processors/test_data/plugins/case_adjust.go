package main

import (
	"bytes"
	"errors"
	"fmt"
	"gravwell" //package expose the builtin plugin funcs

	"github.com/gravwell/gravwell/v4/ingest/entry"
)

const (
	PluginName = "recase"
)

var (
	cfg   CaseConfig
	tg    gravwell.Tagger
	ready bool

	ErrNotReady = errors.New("not ready")
)

type CaseConfig struct {
	Upper bool
	Lower bool
}

func nop() error {
	return nil //this is a synchronous plugin, so no "start" or "close"
}

func Config(cm gravwell.ConfigMap, tgr gravwell.Tagger) (err error) {
	if cm == nil || tgr == nil {
		err = errors.New("bad parameters")
	}
	cfg.Upper, _ = cm.GetBool("upper")
	cfg.Lower, _ = cm.GetBool("lower")

	if cfg.Upper && cfg.Lower {
		err = errors.New("upper and lower case are exclusive")
	} else if !cfg.Upper && !cfg.Lower {
		err = errors.New("at least one upper/lower config must be set")
	} else {
		tg = tgr
		ready = true
	}
	return
}

func Flush() []*entry.Entry {
	return nil //we don't hold on to anything
}

func Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	if !ready {
		return nil, ErrNotReady
	}
	if cfg.Upper {
		for i := range ents {
			ents[i].Data = bytes.ToUpper(ents[i].Data)
		}
	} else if cfg.Lower {
		for i := range ents {
			ents[i].Data = bytes.ToLower(ents[i].Data)
		}
	}
	return ents, nil
}

func main() {
	if err := gravwell.Execute(PluginName, Config, nop, nop, Process, Flush); err != nil {
		panic(fmt.Sprintf("Failed to execute dynamic plugin %s - %v\n", PluginName, err))
	}
}
