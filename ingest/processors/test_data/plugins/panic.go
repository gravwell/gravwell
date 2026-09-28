package main

import (
	"errors"
	"gravwell" //package expose the builtin plugin funcs

	"github.com/gravwell/gravwell/v4/ingest/entry"
)

const (
	PluginName = "panic"
)

var (
	tg    gravwell.Tagger
	ready bool

	ErrNotReady = errors.New("not ready")
)

func nop() error {
	return nil
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
	panic("AAAAAAH. HELP ME")
}
