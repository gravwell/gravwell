/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"bytes"
	"encoding/binary"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/utils/jsoncompat"
)

func TestStreamConfigurationEncodeDecode(t *testing.T) {
	b := make([]byte, 1)
	var cfg StreamConfiguration
	if err := cfg.decode(b); err != nil {
		t.Fatal(err)
	}
	b[0] = byte(CompressSnappy)
	if err := cfg.decode(b); err != nil {
		t.Fatal(err)
	} else if cfg.Compression != CompressSnappy {
		t.Fatal("Failed to decode value")
	}

	b[0] = 0xff
	if err := cfg.decode(b); err == nil {
		t.Fatal("Failed to catch bad compression value")
	}
}

func TestOversizedStreamConfigurationEncodeDecode(t *testing.T) {
	b := make([]byte, 1024)
	var cfg StreamConfiguration
	if err := cfg.decode(b); err != nil {
		t.Fatal(err)
	}
	b[0] = byte(CompressSnappy)
	if err := cfg.decode(b); err != nil {
		t.Fatal(err)
	} else if cfg.Compression != CompressSnappy {
		t.Fatal("Failed to decode value")
	}

	b[0] = 0xff
	if err := cfg.decode(b); err == nil {
		t.Fatal("Failed to catch bad compression value")
	}
}

func TestStreamConfiguration(t *testing.T) {
	bb := bytes.NewBuffer(make([]byte, 0, 64))
	x := StreamConfiguration{
		Compression: CompressSnappy,
	}
	var y StreamConfiguration
	if err := x.Write(bb); err != nil {
		t.Fatal(err)
	}
	if err := y.Read(bb); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(x, y) {
		t.Fatalf("ReadWrite failure: %+v != %+v\n", x, y)
	}

	//reset with a larger buffer being thrown
	buff := make([]byte, 64+4)
	binary.LittleEndian.PutUint32(buff, 64)
	buff[4] = byte(CompressSnappy)
	bb = bytes.NewBuffer(buff)
	if err := y.Read(bb); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(x, y) {
		t.Fatalf("ReadWrite failure: %+v != %+v\n", x, y)
	}
}

func TestIngestState(t *testing.T) {
	bb := bytes.NewBuffer(make([]byte, 0, 64))
	x := IngesterState{
		Name:     "foobar",
		Tags:     []string{},
		Children: map[string]IngesterState{},
	}
	var y IngesterState
	if err := x.Write(bb); err != nil {
		t.Fatal(err)
	}
	if err := y.Read(bb); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(x, y) {
		t.Fatalf("ReadWrite failure: %+v != %+v\n", x, y)
	}
}

// writeRawIngesterState encodes an IngesterState onto the wire without the size
// enforcement that IngesterState.Write applies, so we can exercise the read-side
// handling of oversized blocks.
func writeRawIngesterState(t *testing.T, s IngesterState) *bytes.Buffer {
	t.Helper()
	data, err := json.Marshal(s, jsoncompat.Options)
	if err != nil {
		t.Fatal(err)
	}
	bb := bytes.NewBuffer(nil)
	if err := binary.Write(bb, binary.LittleEndian, uint32(len(data))); err != nil {
		t.Fatal(err)
	}
	if _, err := bb.Write(data); err != nil {
		t.Fatal(err)
	}
	return bb
}

// A state that is larger than maxIngestStateSize but not absurd should be read
// off the wire and discarded (not reported), leaving the stream synchronized so
// the connection survives.
func TestOversizedIngestStateDiscarded(t *testing.T) {
	bigCfg, err := json.Marshal(strings.Repeat("A", int(maxIngestStateSize)+1024), jsoncompat.Options)
	if err != nil {
		t.Fatal(err)
	}
	x := IngesterState{
		Name:          "foobar",
		Tags:          []string{"tag1", "tag2"},
		Configuration: jsontext.Value(bigCfg),
		Metadata:      jsontext.Value(bigCfg),
		Children:      map[string]IngesterState{},
	}
	bb := writeRawIngesterState(t, x)
	if uint32(bb.Len()) <= maxIngestStateSize {
		t.Fatalf("test setup failure: expected an oversized block, got %d", bb.Len())
	}
	// tack on a trailing sentinel to prove Read consumed exactly the state bytes
	// and left the stream synchronized
	const sentinel = "SENTINEL"
	bb.WriteString(sentinel)

	var y IngesterState
	if err := y.Read(bb); err != nil {
		t.Fatalf("oversized-but-sane state should be discarded, not error: %v", err)
	}
	// the oversized report is dropped entirely, nothing populated
	if !reflect.DeepEqual(y, IngesterState{}) {
		t.Fatalf("expected oversized state to be discarded, got %+v", y)
	}
	// and the stream must be left exactly at the sentinel
	if rest := bb.String(); rest != sentinel {
		t.Fatalf("stream not left synchronized after discard: %q", rest)
	}
}

// A state block claiming an absurd size must be rejected outright so the caller
// can tear down the connection.
func TestStupidSizedIngestStateRejected(t *testing.T) {
	bb := bytes.NewBuffer(nil)
	if err := binary.Write(bb, binary.LittleEndian, maxIngestStateStupidSize+1); err != nil {
		t.Fatal(err)
	}
	var y IngesterState
	if err := y.Read(bb); err != ErrOversizedIngestState {
		t.Fatalf("expected ErrOversizedIngestState, got %v", err)
	}
}

// TestIngesterStateCopyIndependence pins what Copy actually detaches.  The indexer
// hands the result to a stats poller that encodes it long after the fact, so a
// caller sorting or rewriting Tags must not reach back into the stored state.
func TestIngesterStateCopyIndependence(t *testing.T) {
	orig := IngesterState{
		UUID: `3f2504e0-4f89-11d3-9a0c-0305e82c3301`,
		Name: `kafka_ingester`,
		Tags: []string{`zeta`, `alpha`, `mike`},
		IP:   net.ParseIP(`10.0.0.42`),
		Children: map[string]IngesterState{
			`child`: {UUID: `child-0`, Tags: []string{`delta`, `bravo`}},
		},
	}
	cp := orig.Copy()

	//sorting the copy in place must not disturb the original
	sort.Strings(cp.Tags)
	if !reflect.DeepEqual(orig.Tags, []string{`zeta`, `alpha`, `mike`}) {
		t.Fatalf("sorting the copy reordered the original: %v", orig.Tags)
	}
	sort.Strings(cp.Children[`child`].Tags)
	if !reflect.DeepEqual(orig.Children[`child`].Tags, []string{`delta`, `bravo`}) {
		t.Fatalf("sorting a child copy reordered the original: %v", orig.Children[`child`].Tags)
	}

	//same for the IP bytes
	cp.IP[0] = 0xff
	if orig.IP.Equal(cp.IP) {
		t.Fatal("writing the copied IP changed the original")
	}

	//a state with no tags or IP should not allocate them into existence
	var empty IngesterState
	if ecp := empty.Copy(); ecp.Tags != nil || ecp.IP != nil {
		t.Fatalf("Copy invented tags/IP: %v %v", ecp.Tags, ecp.IP)
	}
}
