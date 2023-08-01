/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package attach

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

type testConfigStruct struct {
	Attach AttachConfig
}

func TestLoadConfig(t *testing.T) {
	var cfg testConfigStruct
	var its []attachItem
	if err := config.LoadConfigBytes(&cfg, []byte(simpleConfig)); err != nil {
		t.Fatal(err)
	} else if its, err = cfg.Attach.Attachments(); err != nil {
		t.Fatal(err)
	} else if len(its) != len(simpleConfigItems) {
		t.Fatalf("invalid attachment count: %d != %d", len(its), len(simpleConfigItems))
	} else {
		for i := range its {
			if err = testKeys(its[i], simpleConfigItems); err != nil {
				t.Fatalf("loaded attachment %d error %v", i, err)
			}
		}
	}
}

func TestLoadBadConfig(t *testing.T) {
	var cfg testConfigStruct
	if err := config.LoadConfigBytes(&cfg, []byte(simpleConfig)); err != nil {
		t.Fatal(err)
	} else if err = cfg.Attach.Verify(); err != nil {
		t.Fatal(err)
	}

	cfg = testConfigStruct{}
	if err := config.LoadConfigBytes(&cfg, []byte(badConfig)); err != nil {
		t.Fatal(err)
	} else if err = cfg.Attach.Verify(); err == nil {
		t.Fatal(err)
	}

	cfg = testConfigStruct{}
	if err := config.LoadConfigBytes(&cfg, []byte(badDupConfig)); err != nil {
		t.Fatal(err)
	} else if err = cfg.Attach.Verify(); err == nil {
		t.Fatal(err)
	}

}

func TestNewAttacher(t *testing.T) {
	var cfg testConfigStruct
	if err := config.LoadConfigBytes(&cfg, []byte(simpleConfig)); err != nil {
		t.Fatal(err)
	}
	guid := uuid.New()
	a, err := NewAttacher(cfg.Attach, guid)
	if err != nil {
		t.Fatal(err)
	} else if a == nil {
		t.Fatal("nil?")
	} else if !a.active {
		t.Fatal("not active")
	} else if !a.haveDynamic {
		t.Fatal("dynamics not set")
	} else if len(a.evs) != len(simpleConfigItems) {
		t.Fatalf("invalid ev count: %v != %v", len(a.evs), len(simpleConfigItems))
	} else if len(a.dynamics) != 1 {
		t.Fatalf("dynamics count is wrong: %d != 1", len(a.dynamics))
	}

	//do it again with an empty config
	if a, err = NewAttacher(AttachConfig{}, guid); err != nil {
		t.Fatal(err)
	} else if a.active || a.haveDynamic || len(a.evs) > 0 || len(a.dynamics) > 0 {
		t.Fatalf("attacher has stuff")
	}
}

func TestAttach(t *testing.T) {
	var cfg testConfigStruct
	guid := uuid.New()
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.LoadConfigBytes(&cfg, []byte(simpleConfig)); err != nil {
		t.Fatal(err)
	}
	a, err := NewAttacher(cfg.Attach, guid)
	if err != nil {
		t.Fatal(err)
	}
	ents := make([]entry.Entry, 16)
	for i := range ents {
		a.Attach(&ents[i])
		time.Sleep(5 * time.Millisecond)
	}

	items := append([]attachItem{}, simpleConfigItems[0:3]...)
	items = append(items, attachItem{key: `hostname`, value: hostname})
	items = append(items, attachItem{key: `uuid`, value: guid.String()})

	for _, ent := range ents {
		if err := testEntConsts(ent, items); err != nil {
			t.Fatal(err)
		}
	}

	//now go swing through the timestamp items and make sure they are all native time types and unique
	times := make(map[entry.Timestamp]bool, len(ents))
	for _, ent := range ents {
		if x, ok := ent.GetEnumeratedValue(`now`); !ok {
			t.Fatal("could not find now value")
		} else if ts, ok := x.(entry.Timestamp); !ok {
			t.Fatalf("now is not a timestamp: %T", x)
		} else if _, ok = times[ts]; ok {
			t.Fatalf("NOW timestamp already exists: %v", ts)
		} else {
			times[ts] = true
		}
	}
}

func TestAttachDuplicate(t *testing.T) {
	var cfg testConfigStruct
	var cfg2 testConfigStruct
	guid := uuid.New()
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.LoadConfigBytes(&cfg, []byte(simpleConfig)); err != nil {
		t.Fatal(err)
	} else if err = config.LoadConfigBytes(&cfg2, []byte(wonkConfig)); err != nil {
		t.Fatal(err)
	}

	a, err := NewAttacher(cfg.Attach, guid)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := NewAttacher(cfg2.Attach, guid)
	if err != nil {
		t.Fatal(err)
	}

	ents := make([]entry.Entry, 16)
	for i := range ents {
		a2.Attach(&ents[i])
		a.Attach(&ents[i])
		time.Sleep(5 * time.Millisecond)
	}

	// remember the order is going to match wonk config, not the regular one
	items := []attachItem{
		attachItem{key: `hostname`, value: hostname},
		attachItem{key: `foo`, value: `bar`},
		attachItem{key: `uuid`, value: guid.String()},
		attachItem{key: `bar`, value: `baz`},
		attachItem{key: `foo-to-the-bar`, value: `this is my foobar, there are many like it, but this one is mine`},
		attachItem{key: `bar`, value: `baz`},
	}

	for _, ent := range ents {
		if err := testEntConsts(ent, items); err != nil {
			t.Fatal(err)
		}
	}

	//now go swing through the timestamp items and make sure they are all native time types and unique
	times := make(map[entry.Timestamp]bool, len(ents))
	for _, ent := range ents {
		if x, ok := ent.GetEnumeratedValue(`now`); !ok {
			t.Fatal("could not find now value")
		} else if ts, ok := x.(entry.Timestamp); !ok {
			t.Fatalf("now is not a timestamp: %T", x)
		} else if _, ok = times[ts]; ok {
			t.Fatalf("NOW timestamp already exists: %v", ts)
		} else {
			times[ts] = true
		}
	}
}

func testEntConsts(ent entry.Entry, items []attachItem) (err error) {
	for _, v := range items {
		if ev, ok := ent.GetEnumeratedValue(v.key); !ok {
			err = fmt.Errorf("Failed to find %v", v.key)
			return
		} else if ev == nil {
			err = fmt.Errorf("ev for key %s is nil", v.key)
			return
		} else if s, ok := ev.(string); !ok {
			err = fmt.Errorf("ev for key %s is not a string %T", v.key, ev)
			return
		} else if v.value != s {
			err = fmt.Errorf("ev for key %s is wrong: %v != %v", v.key, v.value, s)
			return
		}
	}
	return
}

func testKeys(v attachItem, set []attachItem) (err error) {
	//go find the value
	for _, s := range set {
		if s.key == v.key {
			if s.value != v.value {
				return fmt.Errorf("key %s value mismatch %v != %v", s.key, s.value, v.value)
			}
			return nil
		}
	}
	return fmt.Errorf("failed to find key %v\n%v", v.key, set)
}

/* DATA BLOBS */
var simpleConfigItems = []attachItem{
	attachItem{key: `foo`, value: `bar`},
	attachItem{key: `bar`, value: `baz`},
	attachItem{key: `baz`, value: `foo`},
	attachItem{key: `hostname`, value: `$HOSTNAME`},
	attachItem{key: `uuid`, value: `$UUID`},
	attachItem{key: `now`, value: `$NOW`},
}

const simpleConfig = `
[Attach]
	foo="bar"
	bar="baz"
	baz="foo"
	hostname=$HOSTNAME
	uuid=$UUID
	now=$NOW
`

const wonkConfig = `
[Attach]
	hostname=$HOSTNAME
	foo="foo to you too"
	uuid=$UUID
	bar="baz round the house"
	foo-to-the-bar="this is my foobar, there are many like it, but this one is mine"
	baz="foo to the bar"
	now=$NOW
`

const badConfig = `
[Attach]
	foo="bar"
	hostname=
	uuid=$UUID
	now=$NOW
`

const badDupConfig = `
[Attach]
	foo="bar"
	foo="baz"
	hostname=$HOSTNAME
	uuid=$UUID
	now=$NOW
`
