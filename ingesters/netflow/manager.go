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

var (
	mtx         sync.Mutex
	connClosers map[int]closer
	connId      int
)

type closer interface {
	Close() error
}

func addConn(c closer) int {
	mtx.Lock()
	connId++
	id := connId
	connClosers[connId] = c
	mtx.Unlock()
	return id
}

func delConn(id int) {
	mtx.Lock()
	delete(connClosers, id)
	mtx.Unlock()
}

func connCount() int {
	mtx.Lock()
	defer mtx.Unlock()
	return len(connClosers)
}
