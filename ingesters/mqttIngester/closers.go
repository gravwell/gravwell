/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"sync"
)

type closer interface {
	Close() error
}

// closers is a small registry that bundles "close everything" with "wait for
// everything". add returns the shared WaitGroup so that each started routine
// can register itself, which makes Close a single call that stops every
// subscriber and blocks until all of their goroutines have exited.
type closers struct {
	mtx sync.Mutex
	wg  sync.WaitGroup
	set []closer
}

func newClosers() *closers {
	return &closers{}
}

func (c *closers) add(v closer) (wg *sync.WaitGroup) {
	if v == nil {
		return nil
	}
	c.mtx.Lock()
	c.set = append(c.set, v)
	wg = &c.wg
	c.mtx.Unlock()
	return
}

func (c *closers) Close() (err error) {
	c.mtx.Lock()
	for _, v := range c.set {
		err = errors.Join(err, v.Close())
	}
	c.wg.Wait()
	c.set = nil
	c.mtx.Unlock()
	return
}
