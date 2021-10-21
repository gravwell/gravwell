package main

import (
	"errors"
	"fmt"
	"gravwell" //package expose the builtin plugin funcs

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	PluginName = "noclose"
)

var (
	tg    gravwell.Tagger
	ready bool

	ErrNotReady = errors.New("not ready")
)

func Start() error {
	return nil
}

func Close() error {
	return errors.New("no Close for you!")
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
	return nil, nil
}

func main() {
	if err := gravwell.Execute(PluginName, Config, Start, Close, Process, Flush); err != nil {
		panic(fmt.Sprintf("Failed to execute dynamic plugin %s - %v\n", PluginName, err))
	}
}
