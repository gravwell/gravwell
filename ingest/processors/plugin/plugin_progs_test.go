// +build !386,!arm,!mips,!mipsle,!s390x

/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package plugin

// a basic valid plugin that does nothing but is fully valid
const basicPlugin = `
package main

import (
	"gravwell"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func main() {
	gravwell.Execute("test", cf, pf, ff)
}

func cf(cm gravwell.ConfigMap, tg gravwell.Tagger) error {
	return nil
}

func ff() []*entry.Entry {
	return nil
}

func pf([]*entry.Entry) ([]*entry.Entry, error) {
	return nil, nil
}
`

// a basic valid program that does not adhere to the plugin structure
// it will not fire the execution system and just exitplugin that does nothing but is fully valid
const basicBadPlugin = `
package main

import (
)

func main() {
	return
}`

// a basic valid program that does not adhere to the plugin structure
// it will not fire the execution system and just exitplugin that does nothing but is fully valid
const badIdlePlugin = `
package main

import (
	"time"
)

func main() {
	for {
		time.Sleep(100*time.Millisecond)
	}
	return
}`

const badPackage = `
package foobar

func foo() {}
`

const empty = ``

const broken = `foobarbaz`

const noMain = `
package main

func foobar() {}
`

const badCall = `
package main

import (
	"gravwell"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func main() {
	gravwell.Execute("test", cf, pf, ff) //ff does not exist
}

func cf(cm gravwell.ConfigMap, tg gravwell.Tagger) error {
	return nil
}

func pf([]*entry.Entry) ([]*entry.Entry, error) {
	return nil, nil
}
`

const recase = `
package main

import (
	"gravwell" //package expose the builtin plugin funcs
	"bytes"
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	PluginName = "recase"
)

var (
	cfg CaseConfig
	tg gravwell.Tagger
	ready bool

	ErrNotReady = errors.New("not ready")
)

type CaseConfig struct {
	Upper bool
	Lower bool
}

func Config(cm gravwell.ConfigMap, tgr gravwell.Tagger) (err error) {
	if cm == nil || tgr == nil {
		err = errors.New("bad parameters")
	}
	cfg.Upper, _ = cm.GetBool("upper")
	cfg.Lower, _ = cm.GetBool("lower")

	if cfg.Upper && cfg.Lower {
		err = errors.New("upper and lower case are exclusive")
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
	if err := gravwell.Execute(PluginName, Config, Process, Flush); err != nil {
		panic(fmt.Sprintf("Failed to execute dynamic plugin %s - %v\n", PluginName, err))
	}
}`
