package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestJsonMaxDate(t *testing.T) {
	si := ShardInfo{
		Start: time.Date(10001, time.January, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(10001, time.January, 2, 0, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(si)
	if err != nil {
		t.Fatal(err)
	}
	var x ShardInfo
	if err := json.Unmarshal(b, &x); err != nil {
		t.Fatal(err)
	} else if x.Start != maxJsonTimestamp || x.End != maxJsonTimestamp {
		t.Fatalf("Invalid timestamps on future shard: %v, %v", x.Start, x.End)
	}

	si = ShardInfo{
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
