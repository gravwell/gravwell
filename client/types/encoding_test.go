package types_test

import (
	"encoding/json"
	"fmt"
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

func TestMarshalingNilCreatesEmpty(t *testing.T) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetIndent("", "\t")

	var tests = []struct {
		data any
	}{
		{types.Dashboard{}},
		//{types.BaseListResponse{}},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T", tt.data), func(t *testing.T) {
			sb.Reset()
			err := enc.Encode(&tt.data)
			require.Nil(t, err)

			out := sb.String()
			if strings.Contains(out, "null") {
				t.Fatal("found null in JSON:\n", out)
			}
		})

	}

}
