/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest_test

import (
	"log"
	"net"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var (
	dst          = "tcp://127.0.0.1:4023"
	sharedSecret = "IngestSecrets"
	simple_tags  = []string{"testtag"}
)

// SimplestExample is the simplest possible example of ingesting a single Entry.
func Example_simplest() {
	// Get an IngestConnection
	igst, err := ingest.InitializeConnection(dst, sharedSecret, simple_tags, "", "", false)
	if err != nil {
		log.Fatalf("Couldn't open connection to ingester: %v", err)
	}
	defer igst.Close()

	// We need to get the numeric value for the tag we're using
	tagid, ok := igst.GetTag(simple_tags[0])
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
