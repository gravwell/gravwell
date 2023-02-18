/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestNewJsonLimitedDecoder(t *testing.T) {
	var obj json.RawMessage
	//check that parameters are tested right
	if lr, err := NewJsonLimitedDecoder(nil, 100); err == nil || lr != nil {
		t.Fatalf("did not catch bad reader: %v %v", err, lr == nil)
	} else if lr, err = NewJsonLimitedDecoder(strings.NewReader("stuff"), 0); err == nil || lr != nil {
		t.Fatalf("did not catch bad limit: %v %v", err, lr == nil)
	} else if lr, err = NewJsonLimitedDecoder(strings.NewReader("stuff"), -1); err == nil || lr != nil {
		t.Fatalf("did not catch bad limit: %v %v", err, lr == nil)
	} else if lr, err = NewJsonLimitedDecoder(nil, -1); err == nil || lr != nil {
		t.Fatalf("did not catch bad limit: %v %v", err, lr == nil)
	}

	str := `{"foo": "this is a string", "bar": 3.14, "baz": 100000}`

	//fire up a good reader and do some testing
	rdr := strings.NewReader(str)
	lr, err := NewJsonLimitedDecoder(rdr, 1024)
	if err != nil {
		t.Fatal(err)
	} else if err = lr.Decode(&obj); err != nil {
		t.Fatal(err)
	} else if !bytes.Equal([]byte(obj), []byte(str)) {
		t.Fatal("output not equal")
	} else if err = lr.Decode(&obj); err != io.EOF {
		t.Fatalf("decode didn't catch EOF: %v", err)
	} else if errors.Is(err, ErrOversizedObject) {
		t.Fatalf("error is wrapped incorrectly: %v", err)
	}

	//new reader with small limit
	rdr = strings.NewReader(str)
	if lr, err = NewJsonLimitedDecoder(rdr, int64(len(str)/2)); err != nil {
		t.Fatal(err)
	} else if err = lr.Decode(&obj); err == nil {
		t.Fatal("failed to catch oversized object")
	} else if !errors.Is(err, ErrOversizedObject) {
		t.Fatalf("returned wrong error: %v", err)
	}
}
