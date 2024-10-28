/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"sort"
	"sync"

	"github.com/gravwell/gravwell/v3/generators/base"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var (
	//prepopulate with our supported ones
	dataTypes = map[string]base.DataGen{
		"binary":   genDataBinary,
		"bind":     genDataBind,
		"csv":      genDataCSV,
		"dnsmasq":  genDataDnsmasq,
		"fields":   genDataFields,
		"json":     genDataJSON,
		"xml":      genDataXML,
		"regex":    genDataRegex,
		"syslog":   genDataSyslog,
		"zeekconn": genDataZeekConn,
		"evs":      genDataEnumeratedValue,
		"megajson": genDataMegaJSON,
	}
	finalizers = map[string]base.Finalizer{
		"evs":      finEnumeratedValue,
		"binary":   fin("binary"),
		"bind":     fin("bind"),
		"csv":      fin("csv"),
		"dnsmasq":  fin("dnsmasq"),
		"fields":   fin("fields"),
		"json":     fin("JSON"),
		"xml":      fin("XML"),
		"regex":    fin("regex"),
		"syslog":   fin("syslog"),
		"zeekconn": fin("zeek conn"),
		"megajson": fin("mega JSON"),
	}

	mtx sync.Mutex
)

func registerDataType(name string, dg base.DataGen, f base.Finalizer) (err error) {
	if name == `` {
		err = errors.New("missing name")
		return
	} else if dg == nil {
		err = errors.New("DataGen function required")
		return
	}
	mtx.Lock()
	defer mtx.Unlock()
	dataTypes[name] = dg
	if f != nil {
		finalizers[name] = f
	}
	return
}

func getGenerator(name string) (dg base.DataGen, f base.Finalizer, ok bool) {
	mtx.Lock()
	defer mtx.Unlock()
	if dg, ok = dataTypes[name]; !ok {
		return
	}
	if f, ok = finalizers[name]; !ok {
		f = emptyFinalizer
		ok = true
	}
	return
}

func getList() (r []string) {
	mtx.Lock()
	defer mtx.Unlock()
	r = make([]string, 0, len(dataTypes))
	for k := range dataTypes {
		r = append(r, k)
	}
	sort.Strings(r)
	return
}

func emptyFinalizer(ent *entry.Entry) {
	return
}

func fin(val string) base.Finalizer {
	return func(ent *entry.Entry) {
		if val != `` {
			ent.AddEnumeratedValueEx("_type", val)
		}
		if *randomSrc {
			ent.SRC = getIP()
		}
	}
}
