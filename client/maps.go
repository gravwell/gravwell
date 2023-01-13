/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"net/http"
	"net/url"
	"sync"
)

type headerMap struct {
	sync.Mutex
	mp map[string]string
}

func newHeaderMap() *headerMap {
	return &headerMap{
		mp: map[string]string{},
	}
}

func (hm *headerMap) add(k, v string) {
	hm.Lock()
	hm.mp[k] = v
	hm.Unlock()
}

func (hm *headerMap) remove(k string) {
	hm.Lock()
	delete(hm.mp, k)
	hm.Unlock()
}

func (hm *headerMap) get(k string) (v string, ok bool) {
	hm.Lock()
	v, ok = hm.mp[k]
	hm.Unlock()
	return
}

func (hm *headerMap) dump() (r map[string]string) {
	hm.Lock()
	r = make(map[string]string, len(hm.mp))
	for k, v := range hm.mp {
		r[k] = v
	}
	hm.Unlock()
	return
}

func (hm *headerMap) populateRequest(hdr http.Header) {
	if hdr == nil {
		return
	}
	hm.Lock()
	for k, v := range hm.mp {
		hdr.Add(k, v)
	}
	hm.Unlock()
	return
}

func (hm *headerMap) duplicate() (r *headerMap) {
	hm.Lock()
	r = newHeaderMap()
	for k, v := range hm.mp {
		r.add(k, v)
	}
	hm.Unlock()
	return
}

type queryMap struct {
	sync.Mutex
	vals url.Values
}

func newQueryMap() *queryMap {
	return &queryMap{
		vals: make(url.Values),
	}
}

func (qm *queryMap) add(k, v string) {
	qm.Lock()
	qm.vals.Add(k, v)
	qm.Unlock()
}

func (qm *queryMap) set(k, v string) {
	qm.Lock()
	qm.vals.Set(k, v)
	qm.Unlock()
}

func (qm *queryMap) remove(k string) {
	qm.Lock()
	qm.vals.Del(k)
	qm.Unlock()
}

func (qm *queryMap) get(k string) (v string, ok bool) {
	var vals []string
	qm.Lock()
	if vals, ok = qm.vals[k]; ok && len(vals) > 0 {
		v = vals[0]
	}
	qm.Unlock()
	return
}

func (qm *queryMap) appendEncode(q string) (r string, err error) {
	//short circuit out on the easy case
	if len(q) == 0 {
		r = qm.encode()
		return
	}

	//we need to append these values
	var vals url.Values
	if vals, err = url.ParseQuery(q); err != nil {
		return
	}
	qm.Lock()
	for k, v := range qm.vals {
		for _, vv := range v {
			vals.Add(k, vv)
		}
	}
	qm.Unlock()
	r = vals.Encode()
	return
}

func (qm *queryMap) encode() (r string) {
	qm.Lock()
	r = qm.vals.Encode()
	qm.Unlock()
	return
}

func (qm *queryMap) dup() (r *queryMap) {
	r = newQueryMap()
	qm.Lock()
	for k, v := range qm.vals {
		for _, vv := range v {
			r.add(k, vv)
		}
	}
	qm.Unlock()
	return
}
