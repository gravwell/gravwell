/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package attach implements the helper functions that allow configuring arbitrary attach directives on ingesters
package attach

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

const (
	nowId  = `$NOW`
	uuidId = `$UUID`
	hostId = `$HOSTNAME`

	envUpdateInterval = time.Minute * 5 //update environment variables every 10minutes
)

type AttachConfig map[string]string

type attachItem struct {
	key   string
	value string
}

func (ac AttachConfig) Attachments() ([]attachItem, error) {
	if len(ac) == 0 {
		return nil, nil //nothing here
	}
	var ats []attachItem
	for name, value := range ac {
		if name == `` {
			return nil, fmt.Errorf("Attach item has an empty name")
		} else if value == `` {
			return nil, fmt.Errorf("Attach item (%s) has an empty value", name)
		}
		ats = append(ats, attachItem{key: name, value: value})
	}
	return ats, nil
}

func (ac AttachConfig) Verify() (err error) {
	_, err = ac.Attachments()
	return
}

type dynamic interface {
	run()
}

type Attacher struct {
	active      bool
	haveDynamic bool
	evs         []entry.EnumeratedValue
	dynamics    []dynamic
}

func NewAttacher(ac AttachConfig, id uuid.UUID) (a *Attacher, err error) {
	var ats []attachItem
	a = &Attacher{}
	if ats, err = ac.Attachments(); err != nil {
		return
	} else if len(ats) == 0 {
		return
	}
	a.evs = make([]entry.EnumeratedValue, len(ats))
	for i, at := range ats {
		a.evs[i].Name = at.key
		switch at.value {
		case hostId:
			// we are not going to dynamically resolve the hostname every time
			// do it once and treat it as a constant
			var hostname string
			if hostname, err = os.Hostname(); err != nil {
				return nil, fmt.Errorf("Attach item %s(%d) failed to get hostname: %v", at.key, i, err)
			}
			a.evs[i].Value = entry.StringEnumData(hostname)
		case uuidId:
			a.evs[i].Value = entry.StringEnumData(id.String())
		case nowId:
			a.haveDynamic = true
			nts := newTimeDynamic(&a.evs[i].Value)
			a.dynamics = append(a.dynamics, nts)
		default:
			if strings.HasPrefix(at.value, `$`) {
				a.haveDynamic = true
				evd := newEnvDynamic(&a.evs[i].Value, at.value, envUpdateInterval)
				a.dynamics = append(a.dynamics, evd)
			} else {
				a.evs[i].Value = entry.StringEnumData(at.value)
			}
		}
	}
	a.active = len(a.evs) > 0
	return
}

func (a *Attacher) Attach(ent *entry.Entry) {
	if a == nil || !a.active {
		return
	} else if a.haveDynamic {
		for _, d := range a.dynamics {
			d.run()
		}
	}
	ent.AddEnumeratedValues(a.evs)
}

func (a *Attacher) Active() bool {
	if a != nil && a.active {
		return true
	}
	return false
}

type timeDynamic struct {
	ed *entry.EnumeratedData
}

func newTimeDynamic(ed *entry.EnumeratedData) dynamic {
	return &timeDynamic{
		ed: ed,
	}
}

func (t timeDynamic) run() {
	*t.ed = entry.TSEnumData(entry.Now())
}

type envDynamic struct {
	key          string
	updateTicker *time.Ticker
	ed           *entry.EnumeratedData
}

func newEnvDynamic(ed *entry.EnumeratedData, envKey string, tckInt time.Duration) dynamic {
	envKey = strings.TrimPrefix(envKey, `$`)
	*ed = entry.StringEnumData(os.Getenv(envKey))
	return &envDynamic{
		key:          envKey,
		updateTicker: time.NewTicker(tckInt), //check updates to the environment variable at most once every 10 min
		ed:           ed,
	}
}

func (e *envDynamic) run() {
	//check if we should update
	select {
	case <-e.updateTicker.C:
		// try to update on our ticker
		if value, ok := os.LookupEnv(e.key); ok {
			*e.ed = entry.StringEnumData(value)
		}
	default: //do nothing
	}
}
