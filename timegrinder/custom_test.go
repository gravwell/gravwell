package timegrinder

import (
	"testing"
	"time"
)

func TestNewPreExtractor(t *testing.T) {
	//make sure a good one works
	pe, err := newPreExtractor(`(\d+)?"foo": (?P<ts>\d+)`)
	if err != nil {
		t.Fatal(err)
	} else if pe.rx == nil {
		t.Fatal("nil rx")
	} else if pe.idx != 2 {
		t.Fatalf("invalid index %d", pe.idx)
	} else if pe.setidx != 4 {
		t.Fatalf("invalid set index %d", pe.setidx)
	}

	//make sure a bad RX fails
	if _, err = newPreExtractor(`(\d+)(?P`); err == nil {
		t.Fatal("failed to catch bad rx")
	} else if _, err = newPreExtractor(`foobar: (?P<one>\d+)-(?P<two>\d+)`); err == nil {
		t.Fatal("Failed to catch two named capture groups")
	} else if _, err = newPreExtractor(`foobar: (\d+)`); err == nil {
		t.Fatal("Failed to catch no named capture groups")
	}
}

func TestPreExtractor(t *testing.T) {

	//test a good extraction
	pe, err := newPreExtractor(`"foo": (?P<ts>\d+)?`)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"stuff": "12345", "foo": 123456}`)
	val, offset := pe.extract(data)
	if len(val) == 0 || offset < 0 {
		t.Fatal("failed to extract")
	} else if string(val) != "123456" {
		t.Fatalf("bad pre-extraction %q != 123456", string(val))
	} else if offset != 26 {
		t.Fatalf("offset is invalid: %d != 26", offset)
	}

	//test one that misses
	data = []byte(`{"stuff": "12345", "foo":"nope, not this one"}`)
	if val, offset = pe.extract(data); val != nil || offset != -1 {
		t.Fatalf("extraction misfire %v %d", val, offset)
	}

	//test one where the main rx hits, but the subrx does not
	data = []byte(`{"stuff": "12345", "foo": }`)
	if val, offset = pe.extract(data); val != nil || offset != -1 {
		t.Fatalf("extraction misfire %v %d", val, offset)
	}
}

func TestBadCustomWithPreExtactor(t *testing.T) {
	// test custom without pre-extract or regex AND a format
	cf := CustomFormat{
		Name:   `bad`,
		Regex:  ``,
		Format: string(UnixSeconds),
	}
	if err := cf.Validate(); err == nil {
		t.Fatal("failed to catch missing pre-extract")
	}

	// test with custom that doesn't match regex
	cf = CustomFormat{
		Name:   `bad`,
		Regex:  AnsiCRegex,
		Format: string(RFC3339),
	}
	if err := cf.Validate(); err == nil {
		t.Fatal("failed to catch regex/format mismatch")
	}

	// test with custom that doesn't match regex
	cf = CustomFormat{
		Name:   `bad`,
		Regex:  AnsiCRegex,
		Format: RFC3339Format,
	}
	if err := cf.Validate(); err == nil {
		t.Fatal("failed to catch regex/format mismatch")
	}

	// test with custom that doesn't have a valid pre-extract
	cf = CustomFormat{
		Name:             `bad`,
		Regex:            ``,
		Format:           string(UnixSeconds),
		Extraction_Regex: `"foo":(\d+),`, //missing named capture group
	}
	if err := cf.Validate(); err == nil {
		t.Fatal("failed to catch missing pre-extract")
	}

	// test with custom that doesn't have a valid pre-extract
	cf = CustomFormat{
		Name:             `bad`,
		Regex:            ``,
		Format:           `2006_01_02_15_04_05`,
		Extraction_Regex: `"foo":(\d+),`, //missing named capture group
	}
	if err := cf.Validate(); err == nil {
		t.Fatal("failed to catch missing pre-extract")
	}
}

func TestWithPreExtractor(t *testing.T) {
	cf := CustomFormat{
		Name:             `embedded unix`,
		Regex:            ``,
		Format:           string(UnixSeconds),
		Extraction_Regex: `"foo":\s*(?P<ts>\d+)`, //missing named capture group
	}
	if err := cf.Validate(); err != nil {
		t.Fatal(err)
	}
	p, err := NewCustomProcessor(cf)
	if err != nil {
		t.Fatal(err)
	}
	ctime, err := time.Parse(time.RFC3339Nano, `2017-11-27T17:09:59Z`)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"stuff": "12345", "foo": 1511802599}`)
	ts, ok, offset := p.Extract(data, time.UTC)
	if !ok {
		t.Fatal("failed extraction")
	} else if offset != 26 {
		t.Fatalf("offset is invalid")
	} else if ctime != ts {
		t.Fatalf("time extraction is bad %v != %v", ts, ctime)
	}

	//try with unix ms
	if ctime, err = time.Parse(time.RFC3339Nano, `2017-11-27T17:09:59.453Z`); err != nil {
		t.Fatal(err)
	}
	cf.Extraction_Regex = `"foo":\s*"(?P<ts>\d+.\d+)"`
	cf.Format = string(UnixMilli)
	if err = cf.Validate(); err != nil {
		t.Fatal(err)
	} else if p, err = NewCustomProcessor(cf); err != nil {
		t.Fatal(err)
	}
	data = []byte(`{"stuff": "12345", "foo":"1511802599.453"}`)
	if ts, ok, offset = p.Extract(data, time.UTC); !ok {
		t.Fatal("failed extraction")
	} else if offset != 26 {
		t.Fatalf("offset is invalid")
	}

	// do some rounding to deal with float drift
	ctime = ctime.Round(time.Millisecond)
	ts = ts.Round(time.Millisecond)
	if ctime != ts {
		t.Fatalf("time extraction is bad %v != %v", ts, ctime)
	}
}

func TestCustom(t *testing.T) {
	cf := CustomFormat{
		Name:   `super custom`,
		Format: `2006_01_02_15_04_05`,
		Regex:  `\d{4}_\d{1,2}_\d{1,2}_\d{1,2}_\d{1,2}_\d{1,2}`,
	}
	if err := cf.Validate(); err != nil {
		t.Fatal(err)
	}
	p, err := NewCustomProcessor(cf)
	if err != nil {
		t.Fatal(err)
	}
	ctime, err := time.Parse(time.RFC3339Nano, `2017-11-27T17:09:59Z`)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`go find this crazy timestamp 2017_11_27_17_09_59`)
	if ts, ok, offset := p.Extract(data, time.UTC); !ok {
		t.Fatal("failed extraction")
	} else if offset != 29 {
		t.Fatalf("offset is invalid: %d != 43", offset)
	} else if ts != ctime {
		t.Fatalf("extracted timestamps are invalid: %v != %v", ts, ctime)
	}
}
func TestCustomWithPreExtactor(t *testing.T) {
	cf := CustomFormat{
		Name:             `super custom`,
		Format:           `2006_01_02_15_04_05`,
		Regex:            `\d{4}_\d{1,2}_\d{1,2}_\d{1,2}_\d{1,2}_\d{1,2}`,
		Extraction_Regex: `"A":\s*"(?P<ts>[^"]+)"`, //missing named capture group
	}
	if err := cf.Validate(); err != nil {
		t.Fatal(err)
	}
	p, err := NewCustomProcessor(cf)
	if err != nil {
		t.Fatal(err)
	}
	ctime, err := time.Parse(time.RFC3339Nano, `2017-11-27T17:09:59Z`)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"NotThisOne": "2021_02_05_09_00_00", "A":"2017_11_27_17_09_59"}`)
	if ts, ok, offset := p.Extract(data, time.UTC); !ok {
		t.Fatal("failed extraction")
	} else if offset != 43 {
		t.Fatalf("offset is invalid: %d != 43", offset)
	} else if ts != ctime {
		t.Fatalf("extracted timestamps are invalid: %v != %v", ts, ctime)
	}

}
