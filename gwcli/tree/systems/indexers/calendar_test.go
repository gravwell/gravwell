//go:build !ci
// +build !ci

/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package indexers

import (
	"path"
	"testing"

	grav "github.com/gravwell/gravwell/v4/client"

	"github.com/gravwell/gravwell/v4/client/objlog"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	. "github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
)

const (
	server = "localhost:80"
	// default user
	defaultUser string = "admin"
	defaultPass string = "changeme"
)

func Test_identifyIndexer(t *testing.T) {
	// setup singletons
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "DEBUG"); err != nil {
		t.Fatalf("%v", err)
	}
	if err := connection.Initialize(server, false, true, path.Join(t.TempDir(), "rest.log")); err != nil {
		panic(err)
	}
	connection.Login(defaultUser, defaultPass, "", true)

	// spawn a test client
	testclient, err := grav.NewOpts(grav.Opts{Server: server, UseHttps: false, InsecureNoEnforceCerts: true, ObjLogger: &objlog.NilObjLogger{}})
	if err != nil {
		t.Skip("failed to create test client for fetching API token: ", err)
	}
	if resp, err := testclient.LoginEx(defaultUser, defaultPass); err != nil {
		t.Skip(err)
	} else if !resp.LoginStatus {
		t.Skip("failed to log test client in: ", resp.Reason)
	}

	// get a random indexer to operate off of
	welldata, err := testclient.WellData()
	if err != nil {
		t.Skip("testclient: ", err)
	} else if len(welldata) == 0 {
		t.Skip("no wells to pull from")
	}
	var (
		//wd          types.IndexerWellData
		indexerName string
	)
	for idxr := range welldata {
		indexerName = idxr
		//wd = w
		break
	}
	// find the expected UUID for this name
	idxStats, err := testclient.GetIndexStats()
	if err != nil {
		t.Skip("testclient: ", err)
	}

	t.Run("identifyIndexer", func(t *testing.T) {
		// try name
		if name, id, err := identifyIndexer(indexerName); err != nil {
			if idxStats[indexerName].Error != err.Error() {
				t.Fatal(err)
			}
			t.Log("matched errors:", err.Error())
		} else if id != idxStats[indexerName].UUID || name != indexerName {
			t.Fatal(ExpectedActual(idxStats[indexerName].UUID, id) + ExpectedActual(indexerName, name))
		}

		// try uuid
		if name, id, err := identifyIndexer(idxStats[indexerName].UUID.String()); err != nil {
			if idxStats[indexerName].Error != err.Error() {
				t.Fatal(err)
			}
			t.Log("matched errors:", err.Error())
		} else if id != idxStats[indexerName].UUID || name != indexerName {
			t.Fatal(ExpectedActual(idxStats[indexerName].UUID, id) + ExpectedActual(indexerName, name))
		}
	})
}
