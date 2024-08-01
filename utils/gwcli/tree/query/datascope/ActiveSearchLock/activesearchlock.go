/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// activesearchlock is a simple package for controlling the singleton locks DS's keepalive
// uses to check for its own viability.
package activesearchlock

import (
	"sync"
	"time"
)

var ( // the shared variables protected by this package
	sIDMtx sync.Mutex
	sID    string
	tsMtx  sync.Mutex
	ts     int64
)

// Atomically gets sid.
func GetSearchID() string {
	sIDMtx.Lock()
	defer sIDMtx.Unlock()
	return sID
}

// Atomically sets sid.
func SetSearchID(sid string) {
	sIDMtx.Lock()
	sID = sid
	sIDMtx.Unlock()
}

// Atomically updates the timestamp to the current unix time.
func UpdateTS() {
	tsMtx.Lock()
	ts = time.Now().Unix()
	tsMtx.Unlock()
}

// Atomically gets the last timestamp.
func GetTS() int64 {
	tsMtx.Lock()
	defer tsMtx.Unlock()
	return ts
}
