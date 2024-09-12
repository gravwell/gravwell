/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package filewatch

import (
	"bytes"
	"context"
	"errors"
	"net"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
	"github.com/gravwell/gravwell/v4/timegrinder"
)

const (
	evFilenameName = `file`
)

type debugOut func(string, ...interface{})

type logger interface {
	Criticalf(string, ...interface{}) error
	Errorf(string, ...interface{}) error
	Warnf(string, ...interface{}) error
	Infof(string, ...interface{}) error
	Debugf(string, ...interface{}) error

	Critical(string, ...rfc5424.SDParam) error
	Error(string, ...rfc5424.SDParam) error
	Warn(string, ...rfc5424.SDParam) error
	Info(string, ...rfc5424.SDParam) error
	Debug(string, ...rfc5424.SDParam) error
}

type LogHandler struct {
	LogHandlerConfig
	tg *timegrinder.TimeGrinder
	w  logWriter
	li *utils.LineIgnorer
}

type LogHandlerConfig struct {
	TagName                 string
	Tag                     entry.EntryTag
	Src                     net.IP
	IgnoreTS                bool
	AssumeLocalTZ           bool
	IgnorePrefixes          []string
	IgnoreGlobs             []string
	TimestampFormatOverride string
	TimezoneOverride        string
	UserTimeRegex           string
	UserTimeFormat          string
	Logger                  logger
	Debugger                debugOut
	Ctx                     context.Context
	TimeFormat              config.CustomTimeFormat
	AttachFilename          bool
	Trim                    bool // run trim space on entries
}

type logWriter interface {
	ProcessContext(*entry.Entry, context.Context) error
}

func NewLogHandler(cfg LogHandlerConfig, w logWriter) (*LogHandler, error) {
	var tg *timegrinder.TimeGrinder
	var err error
	if w == nil {
		return nil, errors.New("output writer is nil")
	}
	if cfg.Logger == nil {
		return nil, errors.New("Logger is nil")
	}
	if !cfg.IgnoreTS {
		tcfg := timegrinder.Config{
			EnableLeftMostSeed: true,
		}
		if tg, err = timegrinder.NewTimeGrinder(tcfg); err != nil {
			return nil, err
		} else if err = cfg.TimeFormat.LoadFormats(tg); err != nil {
			return nil, err
		}
		if cfg.TimestampFormatOverride != `` {
			if err = tg.SetFormatOverride(cfg.TimestampFormatOverride); err != nil {
				return nil, err
			}
		}
		if cfg.Debugger != nil {
			cfg.Debugger("Loaded %d custom time formats\n", len(cfg.TimeFormat))
		}
		if cfg.AssumeLocalTZ && cfg.TimezoneOverride != `` {
			return nil, errors.New("Cannot specify AssumeLocalTZ and TimezoneOverride in the same LogHandlerConfig")
		}
		if cfg.AssumeLocalTZ {
			tg.SetLocalTime()
		}
		if cfg.TimezoneOverride != `` {
			err = tg.SetTimezone(cfg.TimezoneOverride)
			if err != nil {
				return nil, err
			}
		}
		if cfg.UserTimeRegex != `` {
			proc, err := timegrinder.NewUserProcessor("user", cfg.UserTimeRegex, cfg.UserTimeFormat)
			if err != nil {
				return nil, err
			}
			if _, err := tg.AddProcessor(proc); err != nil {
				return nil, err
			}
		}
	}
	if !cfg.IgnoreTS && tg == nil {
		return nil, errors.New("no timegrinder but not ignoring timestamps")
	}

	li, err := utils.NewIgnorer(cfg.IgnorePrefixes, cfg.IgnoreGlobs)
	if err != nil {
		return nil, err
	}

	return &LogHandler{
		LogHandlerConfig: cfg,
		w:                w,
		tg:               tg,
		li:               li,
	}, nil
}

func (lh *LogHandler) Tag() string {
	return lh.LogHandlerConfig.TagName
}

func (lh *LogHandler) HandleLog(b []byte, catchts time.Time, fname string) error {
	if len(b) == 0 {
		return nil
	}
	var ok bool
	var ts time.Time
	var err error

	if lh.Trim {
		if b = bytes.TrimSpace(b); len(b) == 0 {
			if lh.Debugger != nil {
				lh.Debugger("Ignoring empty line after trim\n")
			}
			return nil
		}
	}

	if lh.li.Ignore(b) {
		return nil
	}

	if !lh.IgnoreTS {
		ts, ok, err = lh.tg.Extract(b)
		if err != nil {
			lh.Logger.Error("catastrophic timegrinder failure", log.KVErr(err))
			return err
		}
	}
	if !ok {
		ts = catchts
	}
	if lh.Debugger != nil {
		lh.Debugger("GOT %s %s\n", ts.Format(time.RFC3339), string(b))
	}
	ent := &entry.Entry{
		SRC:  lh.Src,
		TS:   entry.FromStandard(ts),
		Tag:  lh.LogHandlerConfig.Tag,
		Data: b,
	}
	if lh.AttachFilename {
		ent.AddEnumeratedValue(entry.EnumeratedValue{
			Name:  evFilenameName,
			Value: entry.StringEnumDataTail(fname),
		})
	}
	return lh.w.ProcessContext(ent, lh.LogHandlerConfig.Ctx)
}
