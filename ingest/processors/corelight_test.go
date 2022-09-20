package processors

import (
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestCorelightConfig(t *testing.T) {
	b := `
	[preprocessor "corelight"]
		type = corelight
	`
	p, err := testLoadPreprocessor(b, `corelight`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the corelight processor
	if c, ok := p.(*Corelight); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *Corelight", p)
	} else {
		if c.Prefix != defaultPrefix {
			t.Fatalf("invalid prefix: %v %v", c.Prefix, defaultPrefix)
		}
	}

	b = `
	[preprocessor "corelight"]
		type = corelight
		Prefix="foobar"
	`
	p, err = testLoadPreprocessor(b, `corelight`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the corelight processor
	if c, ok := p.(*Corelight); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *Corelight", p)
	} else {
		if c.Prefix != "foobar" {
			t.Fatalf("invalid prefix: %v foobar", c.Prefix)
		}
	}

}

func TestCorelightTransitions(t *testing.T) {
	b := `
	[preprocessor "corelight"]
		type = corelight
	`
	p, err := testLoadPreprocessor(b, `corelight`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the corelight processor
	c, ok := p.(*Corelight)
	if !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *Corelight", p)
	}
	var ent entry.Entry
	for i, v := range corelightTestData {
		ent.Data = []byte(v.input)
		if ents, err := c.Process([]*entry.Entry{&ent}); err != nil {
			t.Fatalf("failed to process %d: %v\n", i, err)
		} else if len(ents) != 1 {
			t.Fatal(`too many entries came out`)
		} else if string(ents[0].Data) != v.output {
			t.Fatalf("Output mismatch %d:\n%s\n%s\n", i, string(ents[0].Data), v.output)
		} else if tn, ok := c.tg.LookupTag(ents[0].Tag); !ok {
			t.Fatal("failed to lookup tag")
		} else if tn != v.tag {
			t.Fatalf("invalid tag: %v != %v", tn, v.tag)
		}
	}
}

type testCheck struct {
	tag    string
	input  string
	output string
}

var corelightTestData = []testCheck{
	testCheck{tag: `zeekftp`, input: ftp1_in, output: ftp1_out},
	testCheck{tag: `zeekftp`, input: ftp2_in, output: ftp2_out},
}
