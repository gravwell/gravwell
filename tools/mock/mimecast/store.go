/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"sync"
)

type store[T any] struct {
	data map[string]*T
	mtx  *sync.RWMutex
}

func newStore[T any]() *store[T] {
	return &store[T]{
		data: make(map[string]*T),
		mtx:  new(sync.RWMutex),
	}
}

func (s *store[T]) get(id string) (*T, bool) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	item, ok := s.data[id]
	return item, ok
}

func (s *store[T]) set(id string, t *T) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.data[id] = t
}
