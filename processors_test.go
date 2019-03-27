/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package timegrinder

import (
	"testing"
)

func TestNewUserProc(t *testing.T) {
	rspStr := `\d\d/\d\d/\d\d\d\d\s\d\d\:\d\d\:\d\d.\d{1,5}`
	fstr := `02/01/2006 15:04:05.99999`
	tstr := `14/12/1984 12:55:33.43212`
	tg, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewUserProcessor(`britishtime`, rspStr, fstr)
	if err != nil {
		t.Fatal(err)
	}
	n, err := tg.AddProcessor(p)
	if err != nil {
		t.Fatal(err)
	}
	if n < 0 {
		t.Fatal("bad add")
	}
	x := []byte(`test ` + tstr)
	tt, ok, err := tg.Extract(x)
	if !ok {
		t.Fatal("Failed")
	} else if err != nil {
		t.Fatal(err)
	} else if tt.Format(fstr) != tstr {
		t.Fatal("times not equiv", tstr, tt.Format(fstr))
	}
}

func TestApacheTZ(t *testing.T) {
	apTz := NewApacheProcessor()
	apNtz := NewApacheNoTZProcessor()

	cand := []byte(`test 14/Mar/2019:12:13:00 -0700`)

	if _, ok, _ := apTz.Extract(cand, nil); !ok {
		t.Fatal("failed extraction")
	} else if _, ok, _ = apNtz.Extract(cand, nil); ok {
		t.Fatal("extrated on tz apache")
	}
}
