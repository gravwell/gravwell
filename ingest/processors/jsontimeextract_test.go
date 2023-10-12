/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"fmt"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestJsonTimestampConfig(t *testing.T) {
	b := `
	[preprocessor "jse"]
		type = jsontimeextract
	`
	if _, err := testLoadPreprocessor(b, `jse`); err == nil {
		t.Fatalf("Failed to catch empty config")
	}
	//check with a bad override
	b = `
	[preprocessor "jse"]
		type = jsontimeextract
		Path=foo.bar
		Timestamp-Override="foobar"
	`
	if _, err := testLoadPreprocessor(b, `jse`); err == nil {
		t.Fatalf("Failed to catch bad override")
	}
	b = `
	[preprocessor "jse"]
		type = jsontimeextract
		Path=foo.bar
	`
	if _, err := testLoadPreprocessor(b, `jse`); err != nil {
		t.Fatal(err)
	}
	b = `
	[preprocessor "jse"]
		type = jsontimeextract
		Path=foo.bar
		Timestamp-Override="RFC3339"
	`
	if _, err := testLoadPreprocessor(b, `jse`); err != nil {
		t.Fatal(err)
	}
	b = `
	[preprocessor "jse"]
		type = jsontimeextract
		Path=foo.bar
		Timestamp-Override="RFC3339"
		Assume-Local-Timezone=true
	`
	p, err := testLoadPreprocessor(b, `jse`)
	if err != nil {
		t.Fatal(err)
	}
	if jp, ok := p.(*JsonTimestamp); !ok {
		t.Fatal("bad processor")
	} else {
		if !jp.Assume_Local_Timezone {
			t.Fatal("didn't set assume local timezone")
		} else if jp.Timestamp_Override != `RFC3339` {
			t.Fatalf("didn't right timestamp override %q", jp.Timestamp_Override)
		} else if len(jp.keys) != 2 {
			t.Fatal("wrong set of keys")
		} else if jp.keys[0] != `foo` || jp.keys[1] != `bar` {
			t.Fatalf("bad keys: %v", jp.keys)
		}
	}

}

func TestJsonTimestamp(t *testing.T) {
	b := `
	[preprocessor "jse"]
		type = jsontimeextract
		Path=foo.bar
		Timestamp-Override="RFC3339"
		Assume-Local-Timezone=true
	`
	p, err := testLoadPreprocessor(b, `jse`)
	if err != nil {
		t.Fatal(err)
	}
	og := time.Date(2020, 12, 15, 12, 1, 2, 3, time.UTC)
	internal := time.Date(2022, 7, 2, 13, 14, 15, 0, time.UTC)
	data := fmt.Sprintf(`{"foo": {"bar": "%s"}}`, internal.Format(time.RFC3339))
	ent := entry.Entry{
		TS:   entry.FromStandard(og),
		Data: []byte(data),
	}
	ret, err := p.Process([]*entry.Entry{&ent})
	if err != nil {
		t.Fatal(err)
	} else if len(ret) != 1 {
		t.Fatalf("wrong return count")
	}
	if !ret[0].TS.StandardTime().Equal(internal) {
		t.Fatalf("invalid processed timestamp: %q != %q", ret[0].TS, internal)
	}

	//now test that its left alone on a miss
	ent.Data = []byte(fmt.Sprintf(`{"bar": {"baz": "%s"}}`, internal.Format(time.RFC3339)))
	ent.TS = entry.FromStandard(og)
	ret, err = p.Process([]*entry.Entry{&ent})
	if err != nil {
		t.Fatal(err)
	} else if len(ret) != 1 {
		t.Fatalf("wrong return count")
	}
	if !ret[0].TS.StandardTime().Equal(og) {
		t.Fatalf("invalid processed timestamp on miss: %q != %q", ret[0].TS, og)
	}
}

func TestJsonTimestampQuoted(t *testing.T) {
	quotedFields := "`\"foo.bar\".bar`"
	b := `
	[preprocessor "jse"]
		type = jsontimeextract
		Path=` + quotedFields + `
		Timestamp-Override="RFC3339"
		Assume-Local-Timezone=true
	`
	p, err := testLoadPreprocessor(b, `jse`)
	if err != nil {
		t.Fatal(err)
	}
	og := time.Date(2020, 12, 15, 12, 1, 2, 3, time.UTC)
	internal := time.Date(2022, 7, 2, 13, 14, 15, 0, time.UTC)
	data := fmt.Sprintf(`{"foo.bar": {"bar": "%s"}}`, internal.Format(time.RFC3339))
	ent := entry.Entry{
		TS:   entry.FromStandard(og),
		Data: []byte(data),
	}
	ret, err := p.Process([]*entry.Entry{&ent})
	if err != nil {
		t.Fatal(err)
	} else if len(ret) != 1 {
		t.Fatalf("wrong return count")
	}
	if !ret[0].TS.StandardTime().Equal(internal) {
		t.Fatalf("invalid processed timestamp: %q != %q", ret[0].TS, internal)
	}

	//now test that its left alone on a miss
	ent.Data = []byte(fmt.Sprintf(`{"bar": {"baz": "%s"}}`, internal.Format(time.RFC3339)))
	ent.TS = entry.FromStandard(og)
	ret, err = p.Process([]*entry.Entry{&ent})
	if err != nil {
		t.Fatal(err)
	} else if len(ret) != 1 {
		t.Fatalf("wrong return count")
	}
	if !ret[0].TS.StandardTime().Equal(og) {
		t.Fatalf("invalid processed timestamp on miss: %q != %q", ret[0].TS, og)
	}
}

func TestJsonTimestampFormats(t *testing.T) {
	b := `
	[preprocessor "jse"]
		type = jsontimeextract
		Path=foo.bar
		Timestamp-Override="%s"
		Assume-Local-Timezone=true
	`
	og := time.Date(2020, 12, 15, 12, 1, 2, 3, time.UTC)
	internal := time.Date(2022, 7, 2, 13, 14, 15, 0, time.UTC)
	config := fmt.Sprintf(b, `RFC3339`)
	//test with RFC3339
	data := fmt.Sprintf(`{"foo": {"bar": "%s"}}`, internal.Format(time.RFC3339))
	if err := testJsonCycle(config, data, og, internal); err != nil {
		t.Fatal(err)
	}

	// test with unix timestamp that is quoted
	data = fmt.Sprintf(`{"foo": {"bar": "%d"}}`, internal.Unix())
	config = fmt.Sprintf(b, `Unix`)
	if err := testJsonCycle(config, data, og, internal); err != nil {
		t.Fatal(err)
	}

	// test with unix timestamp that is NOT quoted
	data = fmt.Sprintf(`{"foo": {"bar": %d}}`, internal.Unix())
	config = fmt.Sprintf(b, `Unix`)
	if err := testJsonCycle(config, data, og, internal); err != nil {
		t.Fatal(err)
	}

	//test as unix ms
	data = fmt.Sprintf(`{"foo": {"bar": %d}}`, internal.Unix()*1000)
	config = fmt.Sprintf(b, `UnixMs`)
	if err := testJsonCycle(config, data, og, internal); err != nil {
		t.Fatal(err)
	}

	//test as unix nano
	data = fmt.Sprintf(`{"foo": {"bar": %d}}`, internal.Unix()*1000000000)
	config = fmt.Sprintf(b, `UnixNano`)
	if err := testJsonCycle(config, data, og, internal); err != nil {
		t.Fatal(err)
	}

	//test as Gravwell format
	ts := internal.UTC()
	data = fmt.Sprintf(`{"foo": {"bar": "%s"}}`, ts.Local().Format(`Jan 02 2006 15:04:05`))
	config = fmt.Sprintf(b, `SyslogVariant`)
	if err := testJsonCycle(config, data, og, ts); err != nil {
		t.Fatal(err)
	}

	//test as unix milli
	ts = internal.Add(120 * time.Millisecond)
	data = fmt.Sprintf(`{"foo": {"bar": %d.12}}`, ts.Unix())
	config = fmt.Sprintf(b, `UnixMilli`)
	if err := testJsonCycle(config, data, og, ts); err != nil {
		t.Fatal(err)
	}

	//test as unix milli with higher precision
	ts = internal.Add(120 * time.Millisecond)
	data = fmt.Sprintf(`{"foo": {"bar": %d.120000}}`, ts.Unix())
	config = fmt.Sprintf(b, `UnixMilli`)
	if err := testJsonCycle(config, data, og, ts); err != nil {
		t.Fatal(err)
	}

	//test as unix milli quoted
	ts = internal.Add(120 * time.Millisecond)
	data = fmt.Sprintf(`{"foo": {"bar": "%d.12"}}`, ts.Unix())
	config = fmt.Sprintf(b, `UnixMilli`)
	if err := testJsonCycle(config, data, og, ts); err != nil {
		t.Fatal(err)
	}
}

func testJsonCycle(config, data string, og, internal time.Time) error {
	p, err := testLoadPreprocessor(config, `jse`)
	if err != nil {
		return err
	}
	ent := entry.Entry{
		TS:   entry.FromStandard(og),
		Data: []byte(data),
	}
	ret, err := p.Process([]*entry.Entry{&ent})
	if err != nil {
		return err
	} else if len(ret) != 1 {
		return fmt.Errorf("wrong return count: %d !+ 1", len(ret))
	}
	if ts := ret[0].TS.StandardTime().UTC(); !ts.Equal(internal) {
		//because go likes to incorporate clock skew into timestamps... we have to have some fuzzy logic to handle this
		min := internal.Add(-10 * time.Millisecond)
		max := internal.Add(10 * time.Millisecond)
		if ts.Before(min) || ts.After(max) {
			return fmt.Errorf("invalid processed timestamp: %q != %q\n\t%v != %v", ts.Format(time.RFC3339), internal.Format(time.RFC3339), ts, internal)
		}
	}
	return nil
}
