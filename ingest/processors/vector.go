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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	VectorProcessor string = `vector`
)

var (
	ErrMissingModel    = errors.New("Missing Model in vector config")
	ErrMissingEndpoint = errors.New("Missing Endpoint in vector config")
	ErrMissingToken    = errors.New("Missing Token in vector config")
	ErrEmptyEmbedding  = errors.New("Empty embedding response")
)

type VectorConfig struct {
	Model    string
	Endpoint string
	Token    string
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
	c.Token = strings.TrimSpace(c.Token)
	if c.Model == "" {
		return ErrMissingModel
	}
	if c.Endpoint == "" {
		return ErrMissingEndpoint
	}
	if c.Token == "" {
		return ErrMissingToken
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
	return &VectorProc{
		VectorConfig: cfg,
		client:       &http.Client{},
	}, nil
}

func (vp *VectorProc) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(VectorConfig); ok {
		if err = cfg.validate(); err == nil {
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
	rset = ents[:0]
	for _, ent := range ents {
		if ent == nil {
			continue
		}
		if err = vp.processEntry(ent); err != nil {
			return
		}
		rset = append(rset, ent)
	}
	return
}

type embeddingRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

type embeddingResult struct {
	Embedding []float64 `json:"embedding"`
	Data      string    `json:"data"`
}

func (vp *VectorProc) processEntry(ent *entry.Entry) error {
	original := string(ent.Data)
	embedding, err := vp.getEmbedding(original)
	if err != nil {
		return err
	}
	result := embeddingResult{
		Embedding: embedding,
		Data:      original,
	}
	ent.Data, err = json.Marshal(result)
	return err
}

func (vp *VectorProc) getEmbedding(text string) ([]float64, error) {
	reqBody, err := json.Marshal(embeddingRequest{
		Input: text,
		Model: vp.Model,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, vp.Endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+vp.Token)

	resp, err := vp.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API returned status %d: %s", resp.StatusCode, string(body))
	}

	var embResp embeddingResponse
	if err = json.Unmarshal(body, &embResp); err != nil {
		return nil, err
	}
	if len(embResp.Data) == 0 || len(embResp.Data[0].Embedding) == 0 {
		return nil, ErrEmptyEmbedding
	}
	return embResp.Data[0].Embedding, nil
}
