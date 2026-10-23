/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package hosted

import "sync"

// StatusTracker is a ready to use implementation of the StatusReporter interface.
// The zero value is usable, embed it in a Runtime implementation to pick up the
// error and warning handling for free.
type StatusTracker struct {
	mtx  sync.RWMutex
	err  error
	warn string
}

// SetError flags the hosted ingester as being in an error state.  The ingester is still alive
// and still trying, this is for conditions like a rejected credential or a remote API that is
// refusing requests.  A nil error clears the condition.
func (st *StatusTracker) SetError(err error) {
	st.mtx.Lock()
	st.err = err
	st.mtx.Unlock()
}

// ClearError clears any error condition, an ingester that recovers is expected to call this.
func (st *StatusTracker) ClearError() {
	st.SetError(nil)
}

// SetWarn flags a condition that is worth surfacing but is not an outright failure, something
// like backing off because the ingest link is unhealthy.  An empty string clears the condition.
func (st *StatusTracker) SetWarn(warn string) {
	st.mtx.Lock()
	st.warn = warn
	st.mtx.Unlock()
}

// ClearWarn clears any warning condition.
func (st *StatusTracker) ClearWarn() {
	st.SetWarn(``)
}

// Status hands back the current error and warning conditions.
func (st *StatusTracker) Status() (warn string, err error) {
	st.mtx.RLock()
	warn, err = st.warn, st.err
	st.mtx.RUnlock()
	return
}
