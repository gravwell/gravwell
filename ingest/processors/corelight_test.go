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

	//test bad prefix
	b = `
	[preprocessor "corelight"]
		type = corelight
		Prefix="foobar:this-that'TheOther"
	`
	p, err = testLoadPreprocessor(b, `corelight`)
	if err == nil {
		t.Fatal("failed to catch bad prefix")
	}

	//test with custom config
	//test bad prefix
	b = `
	[preprocessor "corelight"]
		type = corelight
		Custom-Format="foobar:ts,this,that,the,other"
		Custom-Format="barbaz:just, one,more , thing "
	`
	p, err = testLoadPreprocessor(b, `corelight`)
	if err != nil {
		t.Fatal("failed to load custom format")
	}
	//cast to the corelight processor
	if c, ok := p.(*Corelight); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *Corelight", p)
	} else {
		if _, ok := c.tags["zeekfoobar"]; !ok {
			t.Fatal("did not load custom tag zeekfoobar")
		} else if _, ok = c.tags["zeekbarbaz"]; !ok {
			t.Fatal("did not load custom tag zeekbarbaz")
		}
		if hdrs, ok := c.tagFields["zeekfoobar"]; !ok || len(hdrs) != 5 {
			t.Fatalf("failed to load zeekfoobar headers: %v", hdrs)
		}
		if hdrs, ok := c.tagFields["zeekbarbaz"]; !ok || len(hdrs) != 4 {
			t.Fatalf("failed to load zeekfoobar headers: %v", hdrs)
		} else if hdrs[1] != "one" || hdrs[2] != "more" || hdrs[3] != "thing" {
			t.Fatalf("invalid header values %v", hdrs)
		}
	}

}

func TestCorelightTransitions(t *testing.T) {
	b := `
	[preprocessor "corelight"]
		type = corelight
		Custom-Format="foobar:ts,this,that,the,other"
		Custom-Format="barbaz:just, one,more , thing "
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
	testCheck{tag: `zeekfoobar`, input: foobar1_in, output: foobar1_out},
	testCheck{tag: `zeekconn`, input: conn1_in, output: conn1_out},
	testCheck{tag: `zeekconn`, input: conn2_in, output: conn2_out},
	testCheck{tag: `zeekdns`, input: dns1_in, output: dns1_out},
	testCheck{tag: `zeekdns`, input: dns2_in, output: dns2_out},
	testCheck{tag: `zeekdhcp`, input: dhcp1_in, output: dhcp1_out},
	testCheck{tag: `zeekftp`, input: ftp1_in, output: ftp1_out},
	testCheck{tag: `zeekftp`, input: ftp2_in, output: ftp2_out},
	testCheck{tag: `zeekssh`, input: ssh1_in, output: ssh1_out},
	testCheck{tag: `zeekssh`, input: ssh2_in, output: ssh2_out},
	testCheck{tag: `zeekssh`, input: ssh3_in, output: ssh3_out},
	testCheck{tag: `zeekssh`, input: ssh4_in, output: ssh4_out},
	testCheck{tag: `zeekhttp`, input: http1_in, output: http1_out},
	testCheck{tag: `zeekfiles`, input: files1_in, output: files1_out},
	testCheck{tag: `zeekssl`, input: ssl1_in, output: ssl1_out},
	testCheck{tag: `zeekssl`, input: ssl2_in, output: ssl2_out},
	testCheck{tag: `zeekx509`, input: x5091_in, output: x5091_out},
	testCheck{tag: `zeeksmtp`, input: smtp1_in, output: smtp1_out},
	testCheck{tag: `zeekpe`, input: pe1_in, output: pe1_out},
	testCheck{tag: `zeekntp`, input: ntp1_in, output: ntp1_out},
	testCheck{tag: `zeekntp`, input: ntp2_in, output: ntp2_out},
	testCheck{tag: `zeeknotice`, input: notice1_in, output: notice1_out},
	testCheck{tag: `zeeknotice`, input: notice2_in, output: notice2_out},
	testCheck{tag: `zeeknotice`, input: notice3_in, output: notice3_out},
	testCheck{tag: `zeekweird`, input: weird1_in, output: weird1_out},
	testCheck{tag: `zeekdpd`, input: dpd1_in, output: dpd1_out},
	testCheck{tag: `zeekirc`, input: irc1_in, output: irc1_out},
	testCheck{tag: `zeekrdp`, input: rdp1_in, output: rdp1_out},
	testCheck{tag: `zeekkerberos`, input: kerberos1_in, output: kerberos1_out},
	testCheck{tag: `zeeksmb_mapping`, input: smb_mapping1_in, output: smb_mapping1_out},
	testCheck{tag: `zeeksmb_files`, input: smb_files1_in, output: smb_files1_out},
	testCheck{tag: `zeektunnel`, input: tunnel1_in, output: tunnel1_out},
	testCheck{tag: `zeeksoftware`, input: software1_in, output: software1_out},
}

// try overriding the x509 parser, make sure overrides work
func TestCorelightOverride(t *testing.T) {
	tag := `zeekx509`
	output := `1600266220.005323	3	CN=www.taosecurity.com`
	b := `
	[preprocessor "corelight"]
		type = corelight
		Custom-Format="x509:ts,certificate.version,certificate.subject"
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
	ent.Data = []byte(x5091_in)
	if ents, err := c.Process([]*entry.Entry{&ent}); err != nil {
		t.Fatalf("failed to process: %v\n", err)
	} else if len(ents) != 1 {
		t.Fatal(`too many entries came out`)
	} else if string(ents[0].Data) != output {
		t.Fatalf("Output mismatch:\n%s\n%s\n", string(ents[0].Data), output)
	} else if tn, ok := c.tg.LookupTag(ents[0].Tag); !ok {
		t.Fatal("failed to lookup tag")
	} else if tn != tag {
		t.Fatalf("invalid tag: %v != %v", tn, tag)
	}
}
