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
