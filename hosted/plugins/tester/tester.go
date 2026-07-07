// Package tester
// A simple test plugin to ensure everything is connected and ingestion works
package tester

import (
	"context"
	"errors"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

const (
	Tag                    string = `test`
	Name                   string = `tester`
	ID                     string = `tester.ingesters.gravwell.io`
	Version                string = `1.0.0` // must be canonical version string with only major.minor.point
	defaultIngesterUUIDStr string = "4f1c35f6-6af6-4103-8fdc-df2c63026f0d"
)

const (
	defaultInterval = time.Second // by default we fire an entry every second
)

type Config struct {
	hosted.BaseConfig
	hosted.SingleTagConfig
	Interval    string // how often to send an entry; must be parsable by time.ParseDuration
	Silent      bool
	Test_Errors bool
}

func (c *Config) Verify() (err error) {
	c.ApplyDefaultIngesterUUID(defaultIngesterUUIDStr)

	if c.Interval != `` {
		if _, err := time.ParseDuration(c.Interval); err != nil {
			return err
		}
	}
	if err := c.VerifyIngesterUUID(); err != nil {
		return err
	}

	return nil
}

func (c *Config) interval() time.Duration {
	dur, err := time.ParseDuration(c.Interval)
	if err != nil || dur <= 0 {
		return defaultInterval
	}
	return dur
}

type TesterIngester struct {
	Config
	tag entry.EntryTag
}

func NewTesterIngester(cfg Config, tn hosted.TagNegotiator) (tt *TesterIngester, err error) {
	if err = cfg.Verify(); err != nil {
		return
	}
	tt = &TesterIngester{
		Config: cfg,
	}
	if tt.tag, err = tn.NegotiateTag(cfg.ResolveTag(Tag)); err != nil {
		return
	}
	return
}

func (tt *TesterIngester) Handle(_ context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	if tt.Silent {
		return hosted.ContinueAfter(tt.interval()), nil
	}

	if err := rt.Write(entry.Entry{
		TS:   entry.Now(),
		Tag:  tt.tag,
		Data: []byte(`test entry`),
	}); err != nil {
		rt.Error("failed to write entry", log.KVErr(err))
	}
	if tt.Test_Errors {
		rt.Error("testing errors", log.KVErr(errors.New("test err")))
	}
	return hosted.ContinueAfter(tt.interval()), nil
}
