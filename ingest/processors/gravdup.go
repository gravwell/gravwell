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
	"encoding/json"
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
	ErrNilGF           = errors.New("GravwellForwarder object is nil")
	ErrFailedTagLookup = errors.New("GravwellForwarder failed to lookup tag")
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
	hot bool
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

func (gf *GravwellForwarder) Flush() []*entry.Entry {
	return nil
}

func (gf *GravwellForwarder) Process(ents []*entry.Entry) (r []*entry.Entry, err error) {
	//on first call, ensure that our muxer connection is hot
	if !gf.hot {
		if err = gf.mxr.WaitForHot(gf.Timeout()); err != nil {
			return
		}
		gf.hot = true
	}
	r = ents
	for _, ent := range ents {
		if ent != nil {
			var ok bool
			lent := *ent // this is so that we don't mutate the tag on the underlying entry
			//lookup the tag to see if we have a translation for it
			if lent.Tag, ok = gf.tm[ent.Tag]; !ok {
				//figure out what the tag name is an try to negotiate it
				var tagname string
				if ent.Tag == entry.GravwellTagId {
					tagname = entry.GravwellTagName
				} else if tagname, ok = gf.tgr.LookupTag(ent.Tag); !ok {
					err = ErrFailedTagLookup
				} else if lent.Tag, err = gf.mxr.NegotiateTag(tagname); err == nil {
					//negotiated, so go ahead and update our local map
					gf.tm[ent.Tag] = lent.Tag
				}
			}
			if err == nil {
				err = gf.mxr.WriteEntry(&lent)
			}
		}
	}
	return
}

// we DO NOT want to ship the ingest secret here, so we mask it off
func (gfc GravwellForwarderConfig) MarshalJSON() ([]byte, error) {
	x := struct {
		config.IngestConfig
		Ingest_Secret string `json:",omitempty"`
	}{
		IngestConfig: gfc.IngestConfig,
	}
	return json.Marshal(x)
}
