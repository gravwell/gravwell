/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

const (
	GravwellForwarderProcessor string = `gravwellforwarder`

	defaultTimeout time.Duration = time.Second * 3
)

var (
	ErrNilGF = errors.New("GravwellForwarder object is nil")
)

type GravwellForwarderConfig struct {
	config.IngestConfig
}

func GravwellForwarderLoadConfig(vc *config.VariableConfig) (c GravwellForwarderConfig, err error) {
	if err = vc.MapTo(&c.IngestConfig); err != nil {
		return
	}
	err = c.Verify()
	return
}

type GravwellForwarder struct {
	GravwellForwarderConfig
	ingest.UniformMuxerConfig
	tm  map[entry.EntryTag]entry.EntryTag
	tgr Tagger
	ctx context.Context
	cf  context.CancelFunc
	mxr *ingest.IngestMuxer
}

func NewGravwellForwarder(cfg GravwellForwarderConfig, tgr Tagger) (*GravwellForwarder, error) {
	if err := cfg.Verify(); err != nil {
		return nil, err
	}
	conns, err := cfg.Targets()
	if err != nil {
		return nil, err
	}
	lmt, err := cfg.RateLimit()
	if err != nil {
		return nil, err
	}
	tgs := tgr.KnownTags()

	mxcfg := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tgs,
		Auth:               cfg.Secret(),
		LogLevel:           cfg.LogLevel(),
		VerifyCert:         !cfg.InsecureSkipTLSVerification(),
		IngesterName:       GravwellForwarderProcessor,
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       uuid.New().String(),
		RateLimitBps:       lmt,
		Logger:             ingest.NoLogger(), //forwarder preprocessor does not support logging
		CacheDepth:         cfg.Cache_Depth,
		CachePath:          cfg.Ingest_Cache_Path,
		CacheSize:          cfg.Max_Ingest_Cache,
		CacheMode:          cfg.Cache_Mode,
	}
	mxr, err := ingest.NewUniformMuxer(mxcfg)
	if err != nil {
		return nil, err
	} else if err = mxr.Start(); err != nil {
		mxr.Close()
		return nil, err
	}
	gf := &GravwellForwarder{
		GravwellForwarderConfig: cfg,
		UniformMuxerConfig:      mxcfg,
		tgr:                     tgr,
		tm:                      make(map[entry.EntryTag]entry.EntryTag, len(tgs)),
		mxr:                     mxr,
	}
	gf.ctx, gf.cf = context.WithCancel(context.Background())

	return gf, nil
}

func (gf *GravwellForwarder) Close() error {
	if gf == nil {
		return ErrNilGF
	}
	gf.cf() //cancel any writes
	if err := gf.mxr.Sync(defaultTimeout); err != nil {
		return err
	}
	return gf.mxr.Close()
}

func (gf *GravwellForwarder) Process(ent *entry.Entry) (r []*entry.Entry, err error) {
	r = []*entry.Entry{ent}
	err = gf.mxr.WriteEntry(ent)
	return
}
