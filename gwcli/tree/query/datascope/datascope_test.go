/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datascope

import (
	"os"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
)

const ( // mock credentials
	user     = "admin"
	password = "changeme"
	server   = "localhost:80"
)

// Tests that the KeepAlive function is actually able to... well... keep a query alive.
// Wakes every so often to check that we can still re-download our search (aka: that it has not expired yet).
// If any of these downloads fail, the test fails.
func TestKeepAlive(t *testing.T) {
	const testIntervals uint = 6

	// connect and login
	// connect to the server for manual calls
	testclient, err := grav.NewOpts(grav.Opts{Server: server, UseHttps: false, InsecureNoEnforceCerts: true})
	if err != nil {
		panic(err)
	}
	if err = testclient.Login(user, password); err != nil {
		panic(err)
	}

	// initialize the log
	if err := clilog.Init("test_log.txt", "DEBUG"); err != nil {
		t.Fatal(err)
	}
	// if the test passed, clean up the log file
	t.Cleanup(func() {
		if !t.Failed() {
			os.Remove("test_log.txt")
		}
	})

	const minDuration = 4 * time.Minute
	// skip this test if we do not have a long enough timeout
	if deadline, unset := t.Deadline(); !unset || !deadline.After(time.Now().Add(minDuration)) {
		t.Skip("this test requires a -timeout of at least ", minDuration)
	}

	// submit a query
	s, err := testclient.StartSearch("tag=gravwell", time.Now().Add(-30*time.Second), time.Now(), false)
	if err != nil {
		t.Fatal("failed to start query:", err)
	}
	t.Cleanup(func() { s.Close() })

	done := make(chan bool)
	t.Cleanup(func() { close(done) })

	// spawn keepalive on it
	go keepAlive(&s, done)

	// pull results from the query after each interval has expired
	testFreq := s.Interval() + time.Second
	for i := range testIntervals {
		time.Sleep(testFreq)

		if _, err := testclient.DownloadSearch(s.ID,
			types.TimeRange{},
			types.DownloadText); err != nil {
			t.Fatalf("failed to download search after %v minutes: %v", i, err)
		}
	}

	// confirm that keepalive is dead by repulling results and expecting a 404
	time.Sleep(30 * time.Second)
	if _, err := testclient.DownloadSearch(s.ID,
		types.TimeRange{},
		uniques.SearchTimeFormat); err == nil {
		t.Fatalf("downloaded search; expected 404:")
	}

}
