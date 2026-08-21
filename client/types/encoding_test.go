package types_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/stretchr/testify/require"
)

func TestJsonMaxDate(t *testing.T) {
	si := types.ShardInfo{
		Start: time.Date(10001, time.January, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(10001, time.January, 2, 0, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(si)
	if err != nil {
		t.Fatal(err)
	}
	var x types.ShardInfo
	if err := json.Unmarshal(b, &x); err != nil {
		t.Fatal(err)
	} else if x.Start != types.MaxJSONTimestamp || x.End != types.MaxJSONTimestamp {
		t.Fatalf("Invalid timestamps on future shard: %v, %v", x.Start, x.End)
	}

	si = types.ShardInfo{
		Start: time.Now(),
		End:   time.Now().Add(1 * time.Hour),
	}
	b, err = json.Marshal(si)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &x); err != nil {
		t.Fatal(err)
	} else if x.Start.Unix() != si.Start.Unix() || x.End.Unix() != si.End.Unix() {
		t.Fatalf("Invalid timestamps on current shard: %v != %v, %v != %v", x.Start, si.Start, x.End, si.End)
	}

}

// tests that every slice/map field on a zero-valued instances of various types marshals as "[]"/"{}"
// rather than "null".
// It recursively walks the type's fields to find Slice/Map-kind fields and tests these fields
// specifically in the output JSON.
//
// It also tests for a related-but-quieter bug:
// If a type's MarshalJSON uses the `type dummyX X` alias trick while embedding another type with a MarshalJSON,
// Go's method promotion hijacks the whole encoding and silently drops every other field.
// This test's reflection traversal will notice the dropped field is missing.
func TestMarshalingNilCreatesEmpty(t *testing.T) {
	var tests = []struct {
		data any
	}{
		{types.Dashboard{}},
		{types.DashboardListResponse{}},
		{types.Macro{}},
		{types.MacroListResponse{}},
		{types.Token{}},
		{types.TokenListResponse{}},
		{types.AX{}},
		{types.AXListResponse{}},
		{types.SavedQuery{}},
		{types.SavedQueryListResponse{}},
		{types.Resource{}},
		{types.ResourceListResponse{}},
		{types.File{}},
		{types.FileListResponse{}},
		{types.Template{}},
		{types.TemplateListResponse{}},
		{types.SearchHistoryEntry{}},
		{types.SearchHistoryListResponse{}},
		{types.UserPreference{}},
		{types.UserPreferenceResponse{}},
		{types.Secret{}},
		{types.SecretListResponse{}},
		{types.ScheduledSearch{}},
		{types.ScheduledSearchListResponse{}},
		{types.ScheduledScript{}},
		{types.ScheduledScriptListResponse{}},
		{types.Flow{}},
		{types.FlowListResponse{}},
		{types.ScheduledSearchResults{}},
		{types.ScheduledSearchResultsListResponse{}},
		{types.ScheduledScriptResults{}},
		{types.ScheduledScriptResultsListResponse{}},
		{types.FlowResults{}},
		{types.FlowResultsListResponse{}},
		{types.Alert{}},
		{types.AlertListResponse{}},
		{types.Playbook{}},
		{types.PlaybookListResponse{}},
		{types.Actionable{}},
		{types.ActionableListResponse{}},
		{types.KitBuildRequest{}},
		{types.KitBuildRequestListResponse{}},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T", tt.data), func(t *testing.T) {
			out, err := json.Marshal(tt.data)
			require.NoError(t, err)

			var back any
			require.NoError(t, json.Unmarshal(out, &back))

			assertNoNullSliceOrMapFields(t, tt.data, back, fmt.Sprintf("%T", tt.data))
		})
	}
}

// assertNoNullSliceOrMapFields recursively checks rv's exported fields against the corresponding
// decoded JSON in parsed.
func assertNoNullSliceOrMapFields(t *testing.T, data any, doubleback any, path string) {
	t.Helper()

	rv := reflect.ValueOf(data)

	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return // a nil pointer to a struct is out of scope
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return
	}

	// If this level of the JSON isn't an object,
	// the value marshaled itself as something else entirely;
	// nothing more to check at this level.
	parsedMap, ok := doubleback.(map[string]any)
	if !ok {
		return
	}

	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.PkgPath != "" {
			continue // unexported
		}

		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}
		name, rest, _ := strings.Cut(jsonTag, ",")
		if name == "" {
			name = field.Name
		}
		omitempty := strings.Contains(","+rest+",", ",omitempty,")

		fv := rv.Field(i)

		if field.Anonymous {
			// Embedded fields are flattened into the same JSON object by default,
			// so keep checking against the same parsedMap.
			assertNoNullSliceOrMapFields(t, fv, doubleback, path)
			continue
		}

		childParsed, present := parsedMap[name]

		switch fv.Kind() {
		case reflect.Slice, reflect.Map:
			if omitempty {
				continue // fine to be entirely absent instead of "[]"/"{}"
			}
			if fv.Kind() == reflect.Slice && fv.Type().Elem().Kind() == reflect.Uint8 {
				continue // []byte should be "", not "[]"
			}
			if !present {
				t.Errorf("%s: field %q is missing from the JSON entirely", path, name)
				continue
			}
			if childParsed == nil {
				t.Errorf("%s: field %q is null, want [] or {}", path, name)
			}
		case reflect.Struct:
			if present {
				assertNoNullSliceOrMapFields(t, fv, childParsed, path+"."+name)
			}
		case reflect.Pointer:
			if present && fv.IsValid() && !fv.IsNil() {
				assertNoNullSliceOrMapFields(t, fv, childParsed, path+"."+name)
			}
		}
	}
}
