/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/klauspost/compress/gzip"
)

func TestVPCLoadConfig(t *testing.T) {
	b := []byte(`
	[global]
	foo = "bar"
	bar = 1337
	baz = 1.337
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[preprocessor "xyz"]
		type = vpc
		Min-Buff-MB=4
		Max-Buff-MB=8
		Extract-JSON=true
	`)
	tc := struct {
		Global struct {
			Foo         string
			Bar         uint16
			Baz         float32
			Foo_Bar_Baz string
		}
		Item map[string]*struct {
			Name  string
			Value int
		}
		Preprocessor ProcessorConfig
	}{}
	if err := config.LoadConfigBytes(&tc, b); err != nil {
		t.Fatal(err)
	}
	var tt testTagger
	p, err := tc.Preprocessor.getProcessor(`xyz`, &tt)
	if err != nil {
		t.Fatal(err)
	}
	if set, err := p.Process(makeEntry(makeCloudWatchLog(4, false), 0)); err != nil {
		t.Fatal(err)
	} else if len(set) != 4 {
		t.Fatalf("Invalid cloud watch log count: %d != 4", len(set))
	} else {
		if err := checkEntryEVs(set); err != nil {
			t.Fatal(err)
		}
	}

	if set, err := p.Process(makeEntry(makeCloudWatchLog(4, true), 0)); err != nil {
		t.Fatal(err)
	} else if len(set) != 4 {
		t.Fatalf("Invalid cloud watch log count: %d != 4", len(set))
	} else {
		if err := checkEntryEVs(set); err != nil {
			t.Fatal(err)
		}
	}

}

type logData struct {
	Owner   string     `json:"owner"`
	Group   string     `json:"logGroup"`
	Stream  string     `json:"logStream"`
	Filters []string   `json:"subscriptionFilters"`
	Type    string     `json:"messageType"`
	Events  []logEvent `json:"logEvents"`
}

type logEvent struct {
	ID   string                 `json:"id"`
	TS   int64                  `json:"timestamp"`
	Msg  string                 `json:"message,omitempty"`
	Flds map[string]interface{} `json:"extractedFields,omitempty"`
}

func makeCloudWatchLog(cnt int, zipped bool) []byte {
	var bb bytes.Buffer
	ld := logData{
		Owner:   `foo`,
		Group:   `bar`,
		Stream:  `baz`,
		Filters: []string{`foo`, `bar`, `baz`},
		Type:    `foo.bar.baz`,
	}
	for i := 0; i < cnt; i++ {
		ld.Events = append(ld.Events, logEvent{
			ID:  fmt.Sprintf("ID%d", i),
			TS:  time.Now().Unix(),
			Msg: fmt.Sprintf("Message %d is zipped %v", i, zipped),
			Flds: map[string]interface{}{
				"start":   `100`,
				"end":     100 + i,
				"message": fmt.Sprintf("%d is zipped %v", i, zipped),
			},
		})
	}
	if !zipped {
		v, _ := json.Marshal(ld)
		return v
	}

	gzw := gzip.NewWriter(&bb)
	if json.NewEncoder(gzw).Encode(ld) != nil {
		return nil
	} else if gzw.Flush() != nil {
		return nil
	} else if gzw.Close() != nil {
		return nil
	}
	return bb.Bytes()
}
