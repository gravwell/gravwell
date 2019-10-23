/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package config

import (
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

	[item C stuff]
	name=testC
	value=	10

	[item D]
	name=` + "`this is a backticked \"`" + `
	value=	100

	[preprocessor "foobar"]
		type = gzip
		foo = 1
		bar = 22
		name = "stuff"
	
	[preprocessor "barbaz"]
		type = regexrouter
		foo-bar-baz = "stuff:(.*)"
	`)
	c, err := OpenBytes(b)
	if err != nil {
		t.Fatal(err)
	} else if c == nil {
		t.Fatal("bad config")
	}
	var v testStruct
	if err = c.Import(&v); err != nil {
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
		t.Fatal("missing C")
	} else if val.Name != "testC" || val.Value != 10 {
		t.Fatal("Item C values are bad", val)
	}

	testVal := "this is a backticked \""
	val, ok = v.Item[`D`]
	if !ok {
		t.Fatal("missing D")
	} else if val.Value != 100 || val.Name != testVal {
		t.Fatalf("Item D values are bad %v != %v", val.Name, testVal)
	}

	//pull back a list of Sections of type preprocessor
	sects, err := c.GetSections(`preprocessor`)
	if err != nil {
		t.Fatal(err)
	}
	if len(sects) != 2 {
		t.Fatal("Bad section count")
	}

	if sects[0].Type() != `preprocessor` || sects[0].Name() != `foobar` {
		t.Fatal("Bad type or name on 0", sects[0].String())
	}
	if sects[1].Type() != `preprocessor` || sects[1].Name() != `barbaz` {
		t.Fatal("Bad type or name on 1", sects[1].String())
	}
	foobar := struct {
		Type string
		Foo  int
		Bar  uint
		Name string
	}{}
	if err := sects[0].MapTo(&foobar); err != nil {
		t.Fatal(err)
	} else if foobar.Type != `gzip` || foobar.Foo != 1 || foobar.Bar != 22 || foobar.Name != "stuff" {
		t.Fatalf("Invalid foobar mapping: %+v", foobar)
	}
	baz := struct {
		Type        string
		Foo_Bar_Baz string
	}{}
	if err := sects[1].MapTo(&baz); err != nil {
		t.Fatal(err)
	} else if baz.Type != `regexrouter` || baz.Foo_Bar_Baz != `stuff:(.*)` {
		t.Fatalf("Invalid barbaz mapping: %+v", baz)
	}
}
