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
		}
	}
}

type testCheck struct {
	tag    string
	input  string
	output string
}

var corelightTestData = []testCheck{
	testCheck{
		tag:    `zeekftp`,
		output: "1597559164.077\tCLkXf2CMo11hD8FQ5\t192.168.4.76\t53380\t196.216.2.24\t21\tanonymous\tftp@example.com\tEPSV\t\t\t\t229\tEntering Extended Passive Mode (|||31746|).\ttrue\t192.168.4.76\t196.216.2.24\t31746",
		input: `
{
  "_path": "ftp",
  "_system_name": "ds61",
  "_write_ts": "2020-08-16T06:26:04.077276Z",
  "_node": "worker-01",
  "ts": "2020-08-16T06:26:03.553287Z",
  "uid": "CLkXf2CMo11hD8FQ5",
  "id.orig_h": "192.168.4.76",
  "id.orig_p": 53380,
  "id.resp_h": "196.216.2.24",
  "id.resp_p": 21,
  "user": "anonymous",
  "password": "ftp@example.com",
  "command": "EPSV",
  "reply_code": 229,
  "reply_msg": "Entering Extended Passive Mode (|||31746|).",
  "data_channel.passive": true,
  "data_channel.orig_h": "192.168.4.76",
  "data_channel.resp_h": "196.216.2.24",
  "data_channel.resp_p": 31746
}`,
	},
}
