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
	// mtx guards access to connClosers and connId.
	mtx sync.Mutex
	// connClosers tracks active connections keyed by their assigned ID.
	connClosers map[int]closer
	// connId is a monotonically increasing counter used to assign unique connection IDs.
	connId int
)

type closer interface {
	Close() error
}

// addConn registers a connection and returns its unique ID.
func addConn(c closer) int {
	mtx.Lock()
	connId++
	id := connId
	connClosers[connId] = c
	mtx.Unlock()
	return id
}

// delConn removes a connection by its ID.
func delConn(id int) {
	mtx.Lock()
	delete(connClosers, id)
	mtx.Unlock()
}

// connCount returns the number of active connections.
func connCount() int {
	mtx.Lock()
	defer mtx.Unlock()
	return len(connClosers)
}

// closeAllConn closes all registered connections.
func closeAllConn() {
	mtx.Lock()
	for _, v := range connClosers {
		v.Close()
	}
	mtx.Unlock()
}
