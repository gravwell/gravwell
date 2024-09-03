package main

import (
	"errors"
	"fmt"
	"gravwell" //package expose the builtin plugin funcs

	"github.com/gravwell/gravwell/v3/ingest/entry"
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
}

func nop() error {
	return nil //this is a synchronous plugin, so no "start" or "close"
}

func Config(cm gravwell.ConfigMap, tgr gravwell.Tagger) (err error) {
	if cm == nil || tgr == nil {
		err = errors.New("bad parameters")
	}
	tg = tgr
	ready = true
	return
}

func Flush() []*entry.Entry {
	return nil //we don't hold on to anything
}

func Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	if !ready {
		return nil, ErrNotReady
	}
	ents[100000000] = nil // this will panic/crash
	return ents, nil
}

func main() {
	if err := gravwell.Execute(PluginName, Config, nop, nop, Process, Flush); err != nil {
		panic(fmt.Sprintf("Failed to execute dynamic plugin %s - %v\n", PluginName, err))
	}
}
