/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

const (
	logCommandPath = `/usr/bin/log`

	minRestartDelay = time.Second
	maxRestartDelay = 30 * time.Second
	// how long to wait for log to exit once its output has ended or it has been told to stop
	exitWaitDelay = 5 * time.Second
	// largest single event accepted, measured as pretty-printed by log
	maxEventSize = 16 * 1024 * 1024
	// longest line of stderr output relayed into the ingester log
	maxStderrLine = 4096
)

// logCommand is the unified logging CLI, a variable so tests can substitute a fake
var logCommand = logCommandPath

type entryHandler interface {
	ProcessContext(*entry.Entry, context.Context) error
}

// streamer runs a `log stream` process, ingesting each event it emits and
// restarting the process with a backoff whenever it exits.
type streamer struct {
	name     string
	args     []string
	tag      entry.EntryTag
	src      net.IP
	ignoreTS bool
	proc     entryHandler
	lg       *log.KVLogger

	minDelay time.Duration
	maxDelay time.Duration

	warnedTS bool
}

func newStreamer(name string, sc *streamConfig, tag entry.EntryTag, src net.IP, proc *processors.ProcessorSet, lg *log.Logger) *streamer {
	return &streamer{
		name:     name,
		args:     sc.args(),
		tag:      tag,
		src:      src,
		ignoreTS: sc.Ignore_Timestamps,
		proc:     proc,
		lg:       log.NewLoggerWithKV(lg, log.KV("stream", name)),
		minDelay: minRestartDelay,
		maxDelay: maxRestartDelay,
	}
}

// run keeps a log process running until ctx is canceled.
func (s *streamer) run(ctx context.Context) {
	delay := s.minDelay
	for {
		start := time.Now()
		n, err := s.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if n > 0 {
			// the process was doing useful work, so bring it straight back
			delay = s.minDelay
		}
		s.lg.Error("log stream stopped, restarting",
			log.KV("events", n), log.KV("runtime", time.Since(start).Round(time.Millisecond)),
			log.KV("delay", delay), log.KVErr(err))

		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		delay = min(delay*2, s.maxDelay)
	}
}

// runOnce runs a single log process to completion, returning the number of
// events ingested and why the process stopped.
func (s *streamer) runOnce(ctx context.Context) (n uint64, err error) {
	stderr := &stderrRelay{lg: s.lg}
	cmd := exec.CommandContext(ctx, logCommand, s.args...)
	cmd.Stderr = stderr
	cmd.WaitDelay = exitWaitDelay
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	if err = cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start %s: %w", logCommand, err)
	}
	s.lg.Info("started log stream", log.KV("pid", cmd.Process.Pid), log.KV("args", strings.Join(s.args, " ")))

	n, err = s.consume(ctx, stdout)

	// Make sure the process is gone before reaping it. If its output ended on its
	// own, give it a moment to exit so we can report its real exit status.
	outputEnded := errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
	var werr error
	if outputEnded {
		t := time.AfterFunc(exitWaitDelay, func() { cmd.Process.Kill() })
		werr = cmd.Wait()
		t.Stop()
	} else {
		cmd.Process.Kill()
		werr = cmd.Wait()
	}
	stderr.flush()

	if ctx.Err() != nil {
		return n, ctx.Err()
	} else if outputEnded {
		if werr != nil {
			return n, fmt.Errorf("log exited: %w", werr)
		}
		return n, errors.New("log exited")
	}
	return n, err
}

// consume ingests events from r until it ends, fails to decode, or ctx is canceled.
func (s *streamer) consume(ctx context.Context, r io.Reader) (n uint64, err error) {
	dec := newEventDecoder(r, maxEventSize, func(line string) {
		s.lg.Info("log stream status", log.KV("output", line))
	})
	for {
		var data []byte
		if data, err = dec.Next(); err != nil {
			if errors.Is(err, errEventTooLarge) {
				err = fmt.Errorf("%w (%d bytes)", err, maxEventSize)
			}
			return
		}
		if err = s.proc.ProcessContext(s.newEntry(data), ctx); err != nil {
			if ctx.Err() != nil {
				return n, ctx.Err()
			}
			// a failure to handle one entry, such as a preprocessor error, is not a reason to restart the stream
			s.lg.Error("failed to send entry", log.KVErr(err))
			continue
		}
		n++
	}
}

func (s *streamer) newEntry(data []byte) *entry.Entry {
	ts := entry.Now()
	if !s.ignoreTS {
		if t, err := eventTimestamp(data); err == nil {
			ts = entry.FromStandard(t)
		} else if !s.warnedTS {
			// warn once rather than on every event if the output format is not what we expect
			s.warnedTS = true
			s.lg.Warn("failed to read event timestamp, using the current time", log.KVErr(err))
		}
	}
	return &entry.Entry{
		TS:   ts,
		SRC:  s.src,
		Tag:  s.tag,
		Data: data,
	}
}

// stderrRelay forwards each line the log process writes to stderr into the ingester log,
// which is where messages such as an invalid predicate or missing privileges show up.
type stderrRelay struct {
	lg  *log.KVLogger
	buf []byte
}

func (w *stderrRelay) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emit(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) >= maxStderrLine {
		w.emit(w.buf)
		w.buf = w.buf[:0]
	}
	w.buf = append([]byte(nil), w.buf...) // drop the consumed prefix
	return len(p), nil
}

func (w *stderrRelay) flush() {
	w.emit(w.buf)
	w.buf = nil
}

func (w *stderrRelay) emit(line []byte) {
	if line = bytes.TrimSpace(line); len(line) > 0 {
		w.lg.Warn("log stream stderr", log.KV("output", string(line[:min(len(line), maxStderrLine)])))
	}
}
