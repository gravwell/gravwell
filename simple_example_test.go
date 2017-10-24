/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest_test

import (
	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	"log"
	"net"
)

// SimplestExample is the simplest possible example of ingesting a single Entry.
func Example_simplest() {
	// Get an IngestConnection to localhost, using the shared secret "IngestSecrets"
	// and specifying that we'll be using the tag "test-tag"
	igst, err := ingest.InitializeConnection("tcp://127.0.0.1:4023", "IngestSecrets", []string{"testtag"}, "", "", false)
	if err != nil {
		log.Fatalf("Couldn't open connection to ingester: %v", err)
	}
	defer igst.Close()

	// We need to get the numeric value for the tag we're using
	tagid, ok := igst.GetTag("testtag")
	if !ok {
		log.Fatal("couldn't look up tag")
	}

	// Now we'll create an Entry
	ent := entry.Entry{
		TS:   entry.Now(),
		SRC:  net.ParseIP("127.0.0.1"),
		Tag:  tagid,
		Data: []byte("This is my test data!"),
	}

	// And finally write the Entry
	igst.WriteEntry(&ent)
}
