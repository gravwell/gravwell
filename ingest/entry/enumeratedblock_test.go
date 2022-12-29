/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package entry

import (
	"bytes"
	"fmt"
	"testing"
)

func TestEnumeratedValueBlockEncodeDecode(t *testing.T) {
	var evb evblock

	for i := 0; i < 32; i++ {
		if ev, err := NewEnumeratedValue(fmt.Sprintf("ev%d", i), int64(i)); err != nil {
			t.Error(err)
		} else {
			evb.Add(ev)
		}
	}

	if !evb.Populated() {
		t.Fatal("evb reported as not populated")
	} else if err := evb.Valid(); err != nil {
		t.Fatal("evb reported not valid", err)
	}

	//encode and decode
	buff, err := evb.Encode()
	if err != nil {
		t.Fatal(err)
	}

	bb := bytes.NewBuffer(nil)
	if _, err = evb.EncodeWriter(bb); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(buff, bb.Bytes()) {
		t.Fatalf("encoding methods do not match")
	}

	//decode using each method
	var teb evblock
	if _, err = teb.Decode(buff); err != nil {
		t.Fatal(err)
	} else if err = teb.Compare(evb); err != nil {
		t.Fatal(err)
	} else if _, err = teb.DecodeReader(bb); err != nil {
		t.Fatal(err)
	} else if err = teb.Compare(evb); err != nil {
		t.Fatal(err)
	}
}
