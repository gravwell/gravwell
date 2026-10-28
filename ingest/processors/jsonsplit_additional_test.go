/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestJsonArraySplitAdditionalFieldsObject(t *testing.T) {
	for _, tc := range []struct {
		name       string
		additional string
		forceJSON  bool
		wantFields string
	}{
		{name: "single string", additional: "host", wantFields: `,"host":"sensor-1"`},
		{name: "single object", additional: "meta", wantFields: `,"meta":{"sequence":7}`},
		{name: "nested number", additional: "meta.sequence", wantFields: `,"sequence":7`},
		{name: "missing field", additional: "missing"},
		{name: "multiple fields", additional: "host,meta.sequence", wantFields: `,"host":"sensor-1","sequence":7`},
		{name: "explicit object", additional: "host", forceJSON: true, wantFields: `,"host":"sensor-1"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := fmt.Sprintf(`[preprocessor "split"]
				Type=jsonarraysplit
				Extraction=events
				Additional-Fields=%q
				Force-JSON-Object=%t
			`, tc.additional, tc.forceJSON)
			p, err := testLoadPreprocessor(cfg, "split")
			if err != nil {
				t.Fatal(err)
			}
			ents := makeEntry([]byte(`{"events":[{"id":1},{"id":2}],"host":"sensor-1","meta":{"sequence":7}}`), 123)
			original := ents[0]
			out, err := p.Process(ents)
			if err != nil {
				t.Fatal(err)
			} else if len(out) != 2 {
				t.Fatalf("got %d entries, want 2", len(out))
			}
			for i, ent := range out {
				var got, want map[string]interface{}
				if err := json.Unmarshal(ent.Data, &got); err != nil {
					t.Errorf("entry %d is not a JSON object: %s: %v", i, ent.Data, err)
					continue
				}
				if err := json.Unmarshal([]byte(fmt.Sprintf(`{"events":{"id":%d}%s}`, i+1, tc.wantFields)), &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("entry %d: got %v, want %v", i, got, want)
				}
				if ent.Tag != original.Tag || ent.TS != original.TS || !ent.SRC.Equal(original.SRC) {
					t.Errorf("entry %d did not preserve metadata", i)
				}
			}
			if err := checkEntryEVs(out); err != nil {
				t.Fatal(err)
			}
		})
	}
}
