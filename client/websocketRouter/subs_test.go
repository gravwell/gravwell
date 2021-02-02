/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package websocketRouter

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// TestSubProtoConnCloseRace tests that the subprotocols can close cleaning when concurrently racing a Close call.
func TestSubProtoConnCloseRace(t *testing.T) {
	chanLen := 50

	for c := 0; c < 100; c++ {
		ch := make(chan json.RawMessage, chanLen)
		spc := &SubProtoConn{
			ch:     ch,
			active: 1,
		}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			for i := 0; i < chanLen; i++ {
				spc.AddMessage(json.RawMessage(fmt.Sprintln(i)))
			}
			wg.Done()
			spc.AddMessage(json.RawMessage("b"))
			spc.AddMessage(json.RawMessage("c"))
		}()
		wg.Wait()
		spc.Close()
	}
}
