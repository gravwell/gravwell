/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package timegrinder

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
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
	n := tg.AddProcessor(p)
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

// TestNewCustomProc shows adding a new custom processor
func TestNewCustomProc(t *testing.T) {
	re := `\d\d/\d\d/\d\d\d\d\s\d\d\:\d\d\:\d\d,\d{1,5}`
	rx, err := regexp.Compile(re)
	if err != nil {
		t.Fatal(err)
	}
	p := &customProc{
		format: `02/01/2006 15:04:05.99999`,
		rxstr:  re,
		rx:     rx,
		name:   `britishtime`,
	}
	tstr := `14/12/1984 12:55:33,43212`
	rstr := `14/12/1984 12:55:33.43212`
	tg, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	n := tg.AddProcessor(p)
	if n < 0 {
		t.Fatal("bad add")
	}
	x := []byte(`test ` + tstr)
	tt, ok, err := tg.Extract(x)
	if !ok {
		t.Fatal("Failed")
	} else if err != nil {
		t.Fatal(err)
	} else if tt.Format(p.Format()) != rstr {
		t.Fatal("times not equiv", tstr, tt.Format(p.Format()))
	}

}

type customProc struct {
	rx     *regexp.Regexp
	rxstr  string
	format string
	name   string
}

func (p *customProc) Format() string {
	return p.format
}

func (p *customProc) ToString(t time.Time) string {
	return t.Format(`02/01/2006 15:04:05`) + "," + fmt.Sprintf("%d", t.Nanosecond()/int(μs))
}

func (p *customProc) ExtractionRegex() string {
	return p.rxstr
}

func (p *customProc) Name() string {
	return p.name
}

func (p *customProc) Extract(d []byte, loc *time.Location) (time.Time, bool, int) {
	idxs := p.rx.FindIndex(d)
	if len(idxs) != 2 {
		return time.Time{}, false, -1
	}

	t, err := p.parse(string(d[idxs[0]:idxs[1]]), loc)
	if err != nil {
		return time.Time{}, false, -1
	}

	return t, true, idxs[0]
}

func (p *customProc) parse(value string, loc *time.Location) (time.Time, error) {
	if i := strings.IndexByte(value, ','); i >= 0 {
		t, err := time.Parse(p.format, value[:i]+"."+value[i+1:])
		if err == nil {
			return t, err
		}
	}
	return time.Parse(p.format, value)
}
