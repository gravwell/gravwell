/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package config

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

type testStruct struct {
	Global struct {
		Foo         string
		Bar         int
		Baz         float64
		Foo_Bar_Baz string
	}
	Item map[string]*struct {
		Name  string
		Value int
	}
	Preprocessor map[string]*VariableConfig
}

var (
	tempDir string
)

func TestMain(m *testing.M) {
	var err error
	if tempDir, err = ioutil.TempDir(os.TempDir(), `config`); err != nil {
		fmt.Println("Failed to make tempdir", err)
		os.Exit(-1)
	}
	r := m.Run()
	if err = os.RemoveAll(tempDir); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create tempdir: %v\n", err)
		os.Exit(-1)
	}
	os.Exit(r)
}

func TestLoad(t *testing.T) {
	b := []byte(`
	[global]
	foo = "bar"
	bar = 1337
	baz = 1.337
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[item "B"]
	name = "bad values"
	value = 0x1000

	[item "B"]
	name = "test B"
	value = 0xB

	[item "C stuff"]
	name=testC
	value=	10

	[item "D"]
	name="this is a quote \""
	value=	100

	[preprocessor "foobar"]
		type = gzip
		foo = 1
		bar = 22
		name = "stuff"
		thing = "thing1"
		thing = "thing2"
	
	[preprocessor "barbaz"]
		type = regexrouter
		foo-bar-baz = "stuff:(.*)"
		bar-baz = 3.14159
	`)
	var v testStruct
	if err := LoadConfigBytes(&v, b); err != nil {
		t.Fatal(err)
	}

	if v.Global.Foo != "bar" || v.Global.Bar != 1337 || v.Global.Baz != 1.337 {
		t.Fatalf("bad global section values:\n%+v", v.Global)
	} else if v.Global.Foo_Bar_Baz != `foo bar baz` {
		t.Fatal("Name mapper failed", v.Global.Foo_Bar_Baz)
	}
	if v.Item == nil {
		t.Fatal("Failed init map")
	}
	val, ok := v.Item[`A`]
	if !ok {
		t.Fatal("missing A")
	} else if val.Name != "test A" || val.Value != 0xA {
		t.Fatal("Item A values are bad", val)
	}
	val, ok = v.Item[`B`]
	if !ok {
		t.Fatal("missing B")
	} else if val.Name != "test B" || val.Value != 0xB {
		t.Fatal("Item B values are bad", val)
	}
	val, ok = v.Item[`C stuff`]
	if !ok {
		t.Fatal("missing c")
	} else if val.Name != "testC" || val.Value != 10 {
		t.Fatal("Item C values are bad", val)
	}

	testVal := `this is a quote "`
	val, ok = v.Item[`D`]
	if !ok {
		t.Fatal("missing D")
	} else if val.Value != 100 || val.Name != testVal {
		t.Fatalf("Item D values are bad %v != %v", val.Name, testVal)
	}

	//pull back a list of Sections of type preprocessor
	//walk the variable configuration section and check the values
	if pp, ok := v.Preprocessor[`foobar`]; !ok {
		t.Fatal("Missing foobar preprocessor")
	} else {
		if sv, ok := pp.get(`Type`); !ok || sv != `gzip` {
			t.Fatal("Bad type")
		}
		if sv, ok := pp.get(`Foo`); !ok || sv != `1` {
			t.Fatal("Bad foo")
		}
		if sv, ok := pp.get(`Bar`); !ok || sv != `22` {
			t.Fatal("Bad bar")
		}
		if sv, ok := pp.get(`Name`); !ok || sv != `stuff` {
			t.Fatal("Bad name")
		}
		//now map it to a struct
		foobar := struct {
			Type  string
			Foo   int16
			Bar   uint32
			Name  string
			Thing []string
		}{}
		if err := pp.MapTo(&foobar); err != nil {
			t.Fatal(err)
		}
		if foobar.Type != `gzip` || foobar.Foo != 1 || foobar.Bar != 22 || foobar.Name != "stuff" {
			t.Fatalf("Invalid foobar mapping: %+v", foobar)
		}
		if len(foobar.Thing) != 2 {
			t.Fatalf("Failed to assign to thing array")
		} else if foobar.Thing[0] != `thing1` {
			t.Fatalf("Thing1 is bad: %s", foobar.Thing[0])
		} else if foobar.Thing[1] != `thing2` {
			t.Fatalf("Thing2 is bad: %s", foobar.Thing[1])
		}

	}
	if pp, ok := v.Preprocessor[`barbaz`]; !ok {
		t.Fatal("missing barbaz preprocessor")
	} else {
		if sv, ok := pp.get(`Type`); !ok || sv != `regexrouter` {
			t.Fatal("Bad type")
		}
		if sv, ok := pp.get(`Foo-Bar-Baz`); !ok || sv != `stuff:(.*)` {
			t.Fatal("Bad foo-bar-baz")
		}
		baz := struct {
			Type        string
			Foo_Bar_Baz string
			Bar_Baz     float32
			Baz         bool
		}{}
		if err := pp.MapTo(&baz); err != nil {
			t.Fatal(err)
		}
		if baz.Type != `regexrouter` || baz.Foo_Bar_Baz != `stuff:(.*)` || baz.Bar_Baz != 3.14159 || baz.Baz {
			t.Fatalf("Invalid barbaz mapping: %+v", baz)
		}
	}
}

type testGlobal struct {
	IngestConfig
	Access_Key_ID     string
	Secret_Access_Key string
}

type testIngesterConfig struct {
	Global testGlobal
	Stream map[string]*struct {
		Stream_Name           string
		Tag                   string
		Region                string
		Iterator_Type         string
		Parse_Time            bool
		Assume_Local_Timezone bool
	}
}

var testConfig = []byte(`
[global]
Ingest-Secret = IngestSecrets
Connection-Timeout = 0
Verify-Remote-Certificates = true
Cleartext-Backend-target=127.0.0.1:4023 #example of adding a cleartext connection
Cleartext-Backend-target=127.1.0.1:4023 #example of adding another cleartext connection
Encrypted-Backend-target=127.1.1.1:4023 #example of adding an encrypted connection
Pipe-Backend-target=/opt/gravwell/comms/pipe #a named pipe connection, this should be used when ingester is on the same machine as a backend
Log-Level=ERROR #options are OFF INFO WARN ERROR

Access-Key-ID=REPLACEMEWITHYOURKEYID
Secret-Access-Key=REPLACEMEWITHYOURKEY

[Stream "stream1"]
	Region="us-west-1"
	Tag=kinesis
	Stream-Name=MyKinesisStreamName	# should be the stream name as AWS knows it
	Iterator-Type=LATEST
	Parse-Time=false
	Assume-Local-Timezone=true
`)

func TestFileLoad(t *testing.T) {
	testFile := filepath.Join(tempDir, `test.cfg`)
	if err := ioutil.WriteFile(testFile, testConfig, 0660); err != nil {
		t.Fatal(err)
	}
	var tc testIngesterConfig
	if err := LoadConfigFile(&tc, testFile); err != nil {
		t.Fatal(err)
	}
	if tc.Global.Ingest_Secret != `IngestSecrets` {
		t.Fatal("bad secret", tc.Global.Ingest_Secret)
	}
	if s, ok := tc.Stream["stream1"]; !ok || s == nil {
		t.Fatal("missing stream1")
	} else {
		if s.Region != `us-west-1` || s.Tag != `kinesis` || s.Stream_Name != `MyKinesisStreamName` {
			t.Fatalf("Bad Stream1: %+v\n", s)
		} else if s.Iterator_Type != `LATEST` || s.Parse_Time || !s.Assume_Local_Timezone {
			t.Fatalf("Bad Stream1: %+v\n", s)
		}
	}
}
