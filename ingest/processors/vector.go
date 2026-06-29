/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	VectorProcessor string = `vector`

	defaultVectorTimeout       = 30 // seconds
	defaultVectorRetryAttempts = 3
	vectorRetryBaseDelay       = 250 * time.Millisecond
	vectorRetryMaxDelay        = 5 * time.Second
)

var (
	ErrMissingModel    = errors.New("Missing Model in vector config")
	ErrMissingEndpoint = errors.New("Missing Endpoint in vector config")
	ErrInvalidEndpoint = errors.New("Invalid Endpoint in vector config")
	ErrEmptyEmbedding  = errors.New("Empty embedding response")
	ErrEmbeddingCount  = errors.New("Embedding response count does not match request")
)

type VectorConfig struct {
	Model                string
	Endpoint             string
	Token                string
	Timeout              int // HTTP timeout in seconds (default: 30)
	Retry_Attempts       int
	Passthrough_On_Error bool // Passthrough entries that fail to embed instead of dropping them
}

func VectorLoadConfig(vc *config.VariableConfig) (c VectorConfig, err error) {
	if err = vc.MapTo(&c); err == nil {
		err = c.validate()
	}
	return
}

func (c *VectorConfig) validate() error {
	c.Model = strings.TrimSpace(c.Model)
	c.Endpoint = strings.TrimSpace(c.Endpoint)

	// Token is optional
	c.Token = strings.TrimSpace(c.Token)

	if c.Model == "" {
		return ErrMissingModel
	}
	if c.Endpoint == "" {
		return ErrMissingEndpoint
	}
	if u, err := url.Parse(c.Endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrInvalidEndpoint
	}
	return nil
}

type VectorProc struct {
	nocloser
	VectorConfig
	client *http.Client
}

func NewVectorProcessor(cfg VectorConfig) (*VectorProc, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultVectorTimeout
	}
	if cfg.Retry_Attempts <= 0 {
		cfg.Retry_Attempts = defaultVectorRetryAttempts
	}
	return &VectorProc{
		VectorConfig: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.Timeout) * time.Second,
		},
	}, nil
}

func (vp *VectorProc) Config(v any) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(VectorConfig); ok {
		if err = cfg.validate(); err == nil {
			if cfg.Retry_Attempts <= 0 {
				cfg.Retry_Attempts = defaultVectorRetryAttempts
			}
			vp.VectorConfig = cfg
		}
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type %T", v)
	}
	return
}

func (vp *VectorProc) Process(ents []*entry.Entry) (rset []*entry.Entry, err error) {
	if len(ents) == 0 {
		return
	}

	// Collect the non-nil entries and their payloads so the whole batch can be
	// embedded in a single request.
	idx := make([]int, 0, len(ents))
	inputs := make([]string, 0, len(ents))
	for i, ent := range ents {
		if ent == nil {
			continue
		}
		idx = append(idx, i)
		inputs = append(inputs, string(ent.Data))
	}
	rset = ents[:0]
	if len(inputs) == 0 {
		return
	}

	embeddings, eerr := vp.getEmbeddings(inputs)
	if eerr != nil {
		// In passthrough mode we keep going quietly and emit the originals
		// untouched. Otherwise surface the error so the ingester logs it; the
		// batch is dropped.
		if vp.Passthrough_On_Error {
			return ents, nil
		}
		return nil, eerr
	}

	for n, i := range idx {
		ent := ents[i]
		// Attach the embedding as a string-encoded intrinsic EV named "embeddings"
		ev, merr := json.Marshal(embeddings[n])
		if merr == nil {
			merr = ent.AddEnumeratedValueEx("embeddings", string(ev))
		}
		if merr != nil {
			if vp.Passthrough_On_Error {
				rset = append(rset, ent)
			}
			continue
		}
		rset = append(rset, ent)
	}
	return
}

type embeddingRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

// getEmbeddings sends the full batch of inputs to an OpenAI-compatible
// embeddings endpoint, retrying transient failures with exponential backoff.
// The returned slice is ordered to match inputs.
func (vp *VectorProc) getEmbeddings(inputs []string) ([][]float64, error) {
	reqBody, err := json.Marshal(embeddingRequest{
		Input: inputs,
		Model: vp.Model,
	})
	if err != nil {
		return nil, err
	}

	attempts := vp.Retry_Attempts
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			time.Sleep(vectorBackoff(attempt))
		}
		embeddings, retryable, derr := vp.doEmbeddingRequest(reqBody, len(inputs))
		if derr == nil {
			return embeddings, nil
		}
		lastErr = derr
		if !retryable {
			break
		}
	}
	return nil, lastErr
}

// doEmbeddingRequest performs a single embedding request. The boolean return
// reports whether the failure is worth retrying (network error, 429, or 5xx).
func (vp *VectorProc) doEmbeddingRequest(reqBody []byte, want int) ([][]float64, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), vp.client.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, vp.Endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if vp.Token != "" {
		req.Header.Set("Authorization", "Bearer "+vp.Token)
	}

	resp, err := vp.client.Do(req)
	if err != nil {
		// Transport-level errors (timeouts, connection refused) are retryable.
		return nil, true, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}

	if resp.StatusCode != http.StatusOK {
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError
		return nil, retryable, fmt.Errorf("embedding API returned status %d: %s", resp.StatusCode, string(body))
	}

	var embResp embeddingResponse
	if err = json.Unmarshal(body, &embResp); err != nil {
		return nil, false, err
	}
	if len(embResp.Data) != want {
		return nil, false, ErrEmbeddingCount
	}

	// The API may return data out of order; place each by its index.
	embeddings := make([][]float64, want)
	for _, d := range embResp.Data {
		if d.Index < 0 || d.Index >= want {
			return nil, false, ErrEmbeddingCount
		}
		if len(d.Embedding) == 0 {
			return nil, false, ErrEmptyEmbedding
		}
		embeddings[d.Index] = d.Embedding
	}
	for _, e := range embeddings {
		if len(e) == 0 {
			return nil, false, ErrEmptyEmbedding
		}
	}
	return embeddings, false, nil
}

// vectorBackoff returns an exponential backoff delay for the given retry attempt
// (attempt >= 1), capped at vectorRetryMaxDelay.
func vectorBackoff(attempt int) time.Duration {
	d := vectorRetryBaseDelay << (attempt - 1)
	if d > vectorRetryMaxDelay || d <= 0 {
		d = vectorRetryMaxDelay
	}
	return d
}
