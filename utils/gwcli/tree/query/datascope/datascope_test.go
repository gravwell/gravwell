/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datascope

import (
	"gwcli/clilog"
	activesearchlock "gwcli/tree/query/datascope/ActiveSearchLock"
	"gwcli/utilities/uniques"
	"os"
	"testing"
	"time"

	grav "github.com/gravwell/gravwell/v3/client"
	"github.com/gravwell/gravwell/v3/client/types"
)

const ( // mock credentials
	user     = "admin"
	password = "changeme"
	server   = "localhost:80"
)

func TestKeepAlive(t *testing.T) {
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
	t.Cleanup(func() { os.Remove("test_log.txt") })

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

	// spawn keepalive on it
	go keepAlive(&s)

	// update the timestamp every 30 seconds, endlessly
	go func() {
		for {
			time.Sleep(30 * time.Second)
			activesearchlock.UpdateTS()
		}
	}()

	// pull results from the query every so often
	for i := 0; i < 3; i++ { // run for 3 minutes
		time.Sleep(time.Minute)

		if _, err := testclient.DownloadSearch(s.ID,
			types.TimeRange{},
			uniques.SearchTimeFormat); err != nil {
			t.Fatalf("failed to download search after %v minutes: %v", i, err)
		}
	}

	// change the sid
	activesearchlock.SetSearchID("different string")

	// confirm that keepalive is dead by repulling results and expecting a 404
	time.Sleep(30 * time.Second)
	if _, err := testclient.DownloadSearch(s.ID,
		types.TimeRange{},
		uniques.SearchTimeFormat); err == nil {
		t.Fatalf("downloaded search; expected 404:")
	}

}
