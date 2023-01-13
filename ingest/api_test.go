/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
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
		Name: "foobar",
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
