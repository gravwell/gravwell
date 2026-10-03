package servicenow

import (
	"context"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

type passThroughTestWriter struct {
	entries []*entry.Entry
}

func (w *passThroughTestWriter) NegotiateTag(string) (entry.EntryTag, error) {
	return 1, nil
}

func (w *passThroughTestWriter) LookupTag(entry.EntryTag) (string, bool) {
	return "servicenow-test", true
}

func (w *passThroughTestWriter) KnownTags() []string {
	return []string{"servicenow-test"}
}

func (w *passThroughTestWriter) WriteEntry(value *entry.Entry) error {
	w.entries = append(w.entries, value)
	return nil
}

func (w *passThroughTestWriter) WriteEntryContext(_ context.Context, value *entry.Entry) error {
	return w.WriteEntry(value)
}

func (w *passThroughTestWriter) WriteBatch(values []*entry.Entry) error {
	w.entries = append(w.entries, values...)
	return nil
}

func (w *passThroughTestWriter) WriteBatchContext(_ context.Context, values []*entry.Entry) error {
	return w.WriteBatch(values)
}

func TestPassThroughProcessorWritesWithoutConfiguredProcessors(t *testing.T) {
	writer := new(passThroughTestWriter)
	processor := newPassThroughProcessor(writer)
	if processor.Count() != 0 {
		t.Fatalf("processor count=%d want=0", processor.Count())
	}
	want := &entry.Entry{Data: []byte(`{"result":"pass-through"}`)}
	if err := processor.ProcessContext(want, context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(writer.entries) != 1 || writer.entries[0] != want {
		t.Fatalf("pass-through writes=%d want=1 exact entry", len(writer.entries))
	}
}
