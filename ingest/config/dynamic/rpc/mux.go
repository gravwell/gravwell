/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// Handler runs one RPC method.  Params is the raw encoded parameters, nil when the caller
// sent none.  The returned value is encoded as the result, a nil result encodes nothing.
// The context is cancelled when the session goes away, so a long running handler can stop
// working on a request nobody is waiting for any more.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Mux is the set of methods one end of a session is willing to serve.  Both ends take
// one, the session is symmetric.  A Mux is safe for concurrent use and may be shared by
// every connection a server accepts.
type Mux struct {
	mtx      sync.RWMutex
	handlers map[string]Handler
}

// NewMux returns an empty Mux.
func NewMux() *Mux {
	return &Mux{handlers: map[string]Handler{}}
}

// Register adds a method.  Registering a method twice is a wiring mistake rather than an
// intent to replace, so it is refused.
func (m *Mux) Register(method string, h Handler) (err error) {
	if m == nil {
		return errors.New("nil mux")
	} else if method == `` {
		return ErrEmptyMethod
	} else if h == nil {
		return fmt.Errorf("nil handler for method %s", method)
	}
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if m.handlers == nil {
		m.handlers = map[string]Handler{}
	} else if _, ok := m.handlers[method]; ok {
		return fmt.Errorf("%w %s", ErrDuplicateMethod, method)
	}
	m.handlers[method] = h
	return
}

// Methods lists the registered method names, for logging and for tests.
func (m *Mux) Methods() (r []string) {
	if m == nil {
		return
	}
	m.mtx.RLock()
	defer m.mtx.RUnlock()
	r = make([]string, 0, len(m.handlers))
	for k := range m.handlers {
		r = append(r, k)
	}
	return
}

// lookup finds a handler.  A nil Mux serves nothing, which is a legitimate configuration
// for an end that only makes calls.
func (m *Mux) lookup(method string) (h Handler, ok bool) {
	if m == nil {
		return
	}
	m.mtx.RLock()
	defer m.mtx.RUnlock()
	h, ok = m.handlers[method]
	return
}
