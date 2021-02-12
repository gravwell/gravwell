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

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

type debugOut func(string, ...interface{})

type logger interface {
	Debug(string, ...interface{}) error
	Info(string, ...interface{}) error
	Warn(string, ...interface{}) error
	Error(string, ...interface{}) error
	Critical(string, ...interface{}) error
}

type LogHandler struct {
	LogHandlerConfig
	tg *timegrinder.TimeGrinder
	w  logWriter
}

type LogHandlerConfig struct {
	Tag                     entry.EntryTag
	Src                     net.IP
	IgnoreTS                bool
	AssumeLocalTZ           bool
	IgnorePrefixes          [][]byte
	TimestampFormatOverride string
	TimezoneOverride        string
	UserTimeRegex           string
	UserTimeFormat          string
	Logger                  logger
	Debugger                debugOut
	Ctx                     context.Context
	TimeFormat              config.CustomTimeFormat
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
		tcfg.FormatOverride = cfg.TimestampFormatOverride
		if tg, err = timegrinder.NewTimeGrinder(tcfg); err != nil {
			return nil, err
		} else if err = cfg.TimeFormat.LoadFormats(tg); err != nil {
			return nil, err
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
	return &LogHandler{
		LogHandlerConfig: cfg,
		w:                w,
		tg:               tg,
	}, nil
}

func (lh *LogHandler) HandleLog(b []byte, catchts time.Time) error {
	if len(b) == 0 {
		return nil
	}
	var ok bool
	var ts time.Time
	var err error
	for _, prefix := range lh.IgnorePrefixes {
		if bytes.HasPrefix(b, prefix) {
			return nil
		}
	}
	if !lh.IgnoreTS {
		ts, ok, err = lh.tg.Extract(b)
		if err != nil {
			lh.Logger.Error("Catastrophic timegrinder failure: %v", err)
			return err
		}
	}
	if !ok {
		ts = catchts
	}
	if lh.Debugger != nil {
		lh.Debugger("GOT %s %s\n", ts.Format(time.RFC3339), string(b))
	}
	return lh.w.ProcessContext(&entry.Entry{
		SRC:  lh.Src,
		TS:   entry.FromStandard(ts),
		Tag:  lh.Tag,
		Data: b,
	}, lh.LogHandlerConfig.Ctx)
}
