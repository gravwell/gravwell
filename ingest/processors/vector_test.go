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
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func makeVectorEntry(data string, tag entry.EntryTag) *entry.Entry {
	return &entry.Entry{
		TS:   entry.Now(),
		Tag:  tag,
		Data: []byte(data),
	}
}

func vectorEmbeddingEndpoint(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}
		auth := r.Header.Get("Authorization")
		expectedAuth := "Bearer test-token"
		if auth != expectedAuth {
			t.Errorf("expected Authorization %q, got %q", expectedAuth, auth)
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if len(req.Input) == 0 {
			t.Errorf("expected at least one input, got none")
		}

		var resp embeddingResponse
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Index     int       `json:"index"`
				Embedding []float64 `json:"embedding"`
			}{
				Index:     i,
				Embedding: []float64{0.1, 0.2, 0.3, 0.4, 0.5},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestVectorLoadConfig(t *testing.T) {
	b := []byte(`
[preprocessor "vec1"]
	type = vector
	Model = test-model
	Endpoint = http://localhost:8080/v1/embeddings
	Token = test-token
`)
	tc := struct {
		Preprocessor ProcessorConfig
	}{}
	if err := config.LoadConfigBytes(&tc, b); err != nil {
		t.Fatal(err)
	}

	var tt testTagger
	p, err := tc.Preprocessor.getProcessor("vec1", &tt)
	if err != nil {
		t.Fatal(err)
	}

	vp, ok := p.(*VectorProc)
	if !ok {
		t.Fatalf("expected *VectorProc, got %T", p)
	}
	if vp.Model != "test-model" {
		t.Errorf("Model mismatch: %q", vp.Model)
	}
	if vp.Endpoint != "http://localhost:8080/v1/embeddings" {
		t.Errorf("Endpoint mismatch: %q", vp.Endpoint)
	}
	if vp.Token != "test-token" {
		t.Errorf("Token mismatch: %q", vp.Token)
	}
	if vp.client == nil {
		t.Fatal("HTTP client is nil")
	}
}

func TestVectorLoadConfigValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr error
	}{
		{
			name: "missing model",
			body: `[preprocessor "vec1"]
type = vector
Endpoint = http://localhost:8080/v1/embeddings
Token = test-token
`,
			wantErr: ErrMissingModel,
		},
		{
			name: "missing endpoint",
			body: `[preprocessor "vec1"]
type = vector
Model = test-model
Token = test-token
`,
			wantErr: ErrMissingEndpoint,
		},
		{
			name: "invalid endpoint",
			body: `[preprocessor "vec1"]
type = vector
Model = test-model
Endpoint = not-a-url
Token = test-token
`,
			wantErr: ErrInvalidEndpoint,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := struct {
				Preprocessor ProcessorConfig
			}{}
			if err := config.LoadConfigBytes(&cfg, []byte(tc.body)); err != nil {
				t.Fatal(err)
			}

			var tt testTagger
			_, err := cfg.Preprocessor.getProcessor("vec1", &tt)
			if err == nil {
				t.Fatalf("expected error %v, got nil", tc.wantErr)
			}
		})
	}
}

func TestVectorProcessor(t *testing.T) {
	ts := vectorEmbeddingEndpoint(t)
	defer ts.Close()

	// Strip the httptest URL prefix from the test server
	endpoint := "http://" + ts.Listener.Addr().String() + "/v1/embeddings"

	cfg := VectorConfig{
		Model:    "test-model",
		Endpoint: endpoint,
		Token:    "test-token",
	}
	p, err := NewVectorProcessor(cfg)
	if err != nil {
		t.Fatal(err)
	}

	input := `{"message": "hello world"}`
	ent := makeVectorEntry(input, 42)

	rset, err := p.Process([]*entry.Entry{ent})
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("expected 1 result, got %d", len(rset))
	}

	// Data is left untouched; the embedding lands in an intrinsic EV.
	if string(rset[0].Data) != input {
		t.Errorf("data should be unchanged: got %q, want %q", string(rset[0].Data), input)
	}
	v, ok := rset[0].GetEnumeratedValue("embeddings")
	if !ok {
		t.Fatal("expected an \"embeddings\" enumerated value")
	}
	embStr, ok := v.(string)
	if !ok {
		t.Fatalf("expected embeddings EV to be a string, got %T", v)
	}
	var embedding []float64
	if err := json.Unmarshal([]byte(embStr), &embedding); err != nil {
		t.Fatalf("embeddings EV is not a valid float slice: %v", err)
	}
	if len(embedding) == 0 {
		t.Error("expected non-empty embedding")
	}
	if rset[0].Tag != 42 {
		t.Errorf("tag mismatch: got %d, want 42", rset[0].Tag)
	}
}

func TestVectorProcessorErrorSurfaced(t *testing.T) {
	// Default (non-passthrough) config: a failing endpoint must surface the
	// error so the ingester can log it, and drop the batch.
	badCfg := VectorConfig{
		Model:          "test-model",
		Endpoint:       "http://localhost:1/unreachable",
		Token:          "bad-token",
		Retry_Attempts: 1,
	}
	badProc, err := NewVectorProcessor(badCfg)
	if err != nil {
		t.Fatal(err)
	}
	badEnt := makeVectorEntry(`{"data": "bad"}`, 2)

	rset, err := badProc.Process([]*entry.Entry{badEnt})
	if err == nil {
		t.Fatal("expected an error to be surfaced on embedding failure, got nil")
	}
	if len(rset) != 0 {
		t.Errorf("expected batch dropped on error, got %d results", len(rset))
	}
}

func TestVectorProcessorNonRetryableRoute(t *testing.T) {
	// An invalid route (404) is non-retryable and must still surface an error.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := VectorConfig{Model: "m", Endpoint: ts.URL + "/wrong/route", Token: "test-token"}
	p, err := NewVectorProcessor(cfg)
	if err != nil {
		t.Fatal(err)
	}

	_, err = p.Process([]*entry.Entry{makeVectorEntry("hi", 1)})
	if err == nil {
		t.Fatal("expected error for invalid route, got nil")
	}
}

func TestVectorProcessorNilEntry(t *testing.T) {
	ts := vectorEmbeddingEndpoint(t)
	defer ts.Close()

	endpoint := "http://" + ts.Listener.Addr().String() + "/v1/embeddings"
	cfg := VectorConfig{
		Model:    "test-model",
		Endpoint: endpoint,
		Token:    "test-token",
	}
	p, err := NewVectorProcessor(cfg)
	if err != nil {
		t.Fatal(err)
	}

	rset, err := p.Process([]*entry.Entry{nil})
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 0 {
		t.Errorf("expected 0 results for nil entry, got %d", len(rset))
	}
}

func TestVectorProcessorEmptyBatch(t *testing.T) {
	cfg := VectorConfig{
		Model:    "test-model",
		Endpoint: "http://localhost:8080/v1/embeddings",
		Token:    "test-token",
	}
	p, err := NewVectorProcessor(cfg)
	if err != nil {
		t.Fatal(err)
	}

	rset, err := p.Process(nil)
	if err != nil {
		t.Fatal(err)
	}
	if rset != nil {
		t.Errorf("expected nil result for empty batch, got %d entries", len(rset))
	}
}

func TestVectorProcessorBatch(t *testing.T) {
	var gotInputs int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		gotInputs = len(req.Input)
		var resp embeddingResponse
		// Return data out of order to exercise index-based reordering.
		for i := len(req.Input) - 1; i >= 0; i-- {
			resp.Data = append(resp.Data, struct {
				Index     int       `json:"index"`
				Embedding []float64 `json:"embedding"`
			}{Index: i, Embedding: []float64{float64(i)}})
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := VectorConfig{Model: "m", Endpoint: ts.URL + "/v1/embeddings", Token: "test-token"}
	p, err := NewVectorProcessor(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ents := []*entry.Entry{
		makeVectorEntry("first", 1),
		makeVectorEntry("second", 2),
		makeVectorEntry("third", 3),
	}
	rset, err := p.Process(ents)
	if err != nil {
		t.Fatal(err)
	}
	if gotInputs != 3 {
		t.Fatalf("expected a single batched request of 3 inputs, server saw %d", gotInputs)
	}
	if len(rset) != 3 {
		t.Fatalf("expected 3 results, got %d", len(rset))
	}
	for i, e := range rset {
		v, ok := e.GetEnumeratedValue("embeddings")
		if !ok {
			t.Fatalf("result %d: missing embeddings EV", i)
		}
		var embedding []float64
		if err := json.Unmarshal([]byte(v.(string)), &embedding); err != nil {
			t.Fatalf("result %d: %v", i, err)
		}
		// Embedding for input i was tagged with index i -> value float64(i).
		if len(embedding) != 1 || embedding[0] != float64(i) {
			t.Errorf("result %d: embedding mismatch, got %v", i, embedding)
		}
	}
}

func TestVectorProcessorPassthroughOnError(t *testing.T) {
	cfg := VectorConfig{
		Model:                "m",
		Endpoint:             "http://localhost:1/v1/embeddings", // unreachable
		Token:                "test-token",
		Retry_Attempts:       1,
		Passthrough_On_Error: true,
	}
	p, err := NewVectorProcessor(cfg)
	if err != nil {
		t.Fatal(err)
	}

	input := `{"message": "keep me"}`
	ent := makeVectorEntry(input, 7)
	rset, err := p.Process([]*entry.Entry{ent})
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("expected entry passed through, got %d results", len(rset))
	}
	if string(rset[0].Data) != input {
		t.Errorf("entry should be unchanged, got %q", string(rset[0].Data))
	}
}

func TestVectorProcessorRetry(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		var req embeddingRequest
		json.NewDecoder(r.Body).Decode(&req)
		var resp embeddingResponse
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Index     int       `json:"index"`
				Embedding []float64 `json:"embedding"`
			}{Index: i, Embedding: []float64{0.5}})
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := VectorConfig{Model: "m", Endpoint: ts.URL + "/v1/embeddings", Token: "test-token", Retry_Attempts: 3}
	p, err := NewVectorProcessor(cfg)
	if err != nil {
		t.Fatal(err)
	}

	rset, err := p.Process([]*entry.Entry{makeVectorEntry("retry me", 1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("expected success after retries, got %d results", len(rset))
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("expected 3 attempts (2 rate-limited + 1 success), got %d", got)
	}
}
