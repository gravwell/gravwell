package tester

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/hosted"
)

const (
	Tag     string = `test`
	Name    string = `tester`
	ID      string = `tester.ingesters.gravwell.io`
	Version string = `1.0.0` // must be canonical version string with only major.minor.point
)

const (
	defaultInterval = time.Second // by default we fire an entry every second
)

type Config struct {
	Ingester_UUID string // set the UUID for the ingester
	Interval      string // how often to send an entry, this should be a string parsable by time.ParseDuration
}

func (c *Config) Verify() (err error) {
	if c.Interval != `` {
		if _, err := time.ParseDuration(c.Interval); err != nil {
			return err
		}
	}
	if c.Ingester_UUID == `` {
		c.Ingester_UUID = uuid.New().String()
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

func (c *Config) UUID() uuid.UUID {
	if c.Ingester_UUID != `` {
		if r, err := uuid.Parse(c.Ingester_UUID); err == nil {
			return r
		}
	}
	return uuid.Nil
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
	if tt.tag, err = tn.NegotiateTag(Tag); err != nil {
		return
	}
	return
}

func (tt *TesterIngester) Run(ctx context.Context, rt hosted.Runtime) (err error) {
	tckr := time.NewTicker(tt.interval())
	defer tckr.Stop()

	rt.Info("starting", log.KV("uuid", tt.UUID()))
mainLoop:
	for {
		select {
		case <-ctx.Done():
			break mainLoop
		case t := <-tckr.C:
			lerr := rt.Write(entry.Entry{
				TS:   entry.FromStandard(t),
				Tag:  tt.tag,
				Data: []byte(`test entry`),
			})
			if lerr != nil {
				rt.Error("tester: failed to write entry: %v", log.KVErr(lerr))
			}
		}
	}
	rt.Info("exiting", log.KV("uuid", tt.UUID()))
	return
}
