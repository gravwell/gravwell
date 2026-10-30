/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package timegrinder

import (
	"testing"
	"time"
)

func TestUKYearZeroTweaked(t *testing.T) {
	tg, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if ts, ok, err := tg.Extract([]byte(`15/06/2024 12:00:00,0`)); err != nil {
		t.Fatal(err)
	} else if !ok || ts.Year() != 2024 {
		t.Fatalf("bad UK extraction: %v %v", ts, ok)
	}

	line := []byte(`15/06/0000 12:00:00,0`)
	if _, _, name, err := tg.DebugExtract(line); err != nil {
		t.Fatal(err)
	} else if name != UK.String() {
		t.Fatalf("expected UK processor, got %q", name)
	}
	ts, ok, err := tg.Extract(line)
	if err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("failed to extract")
	} else if yr := ts.Year(); yr != time.Now().Year() && yr != time.Now().Year()-1 {
		t.Fatalf("year zero timestamp was not tweaked: %v", ts)
	}
}
