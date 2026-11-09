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
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

const (
	logCommandPath = `/usr/bin/log`

	minRestartDelay = time.Second
	maxRestartDelay = 30 * time.Second
	// how long a log process must run before an exit is treated as healthy and the
	// restart backoff resets to minDelay, rather than carrying the backoff forward.
	// A process that ingests an event but exits again moments later (e.g. a poison
	// event that keeps hitting errEventTooLarge, or malformed JSON following a few
	// good events) should keep backing off instead of resetting every cycle.
	minHealthyRuntime = 30 * time.Second
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

// processorCloser is satisfied by *processors.ProcessorSet. It's declared as an
// interface, rather than runStreams using the concrete type directly, so tests can
// substitute a stub to observe Close calls without building real preprocessors.
type processorCloser interface {
	entryHandler
	Close() error
}

// streamer runs a `log stream` process, ingesting each event it emits and
// restarting the process with a backoff whenever it exits.
type streamer struct {
	name     string
	args     []string
	tag      entry.EntryTag
	src      net.IP
	host     string
	ignoreTS bool
	proc     entryHandler
	lg       *log.KVLogger

	minDelay   time.Duration
	maxDelay   time.Duration
	minHealthy time.Duration
	exitDelay  time.Duration

	warnedTS bool
}

func newStreamer(name string, sc *streamConfig, tag entry.EntryTag, globalSrc net.IP, host string, proc processorCloser, lg *log.Logger) *streamer {
	return &streamer{
		name:       name,
		args:       sc.args(),
		tag:        tag,
		src:        resolveSource(sc, globalSrc),
		host:       host,
		ignoreTS:   sc.Ignore_Timestamps,
		proc:       proc,
		lg:         log.NewLoggerWithKV(lg, log.KV("stream", name)),
		minDelay:   minRestartDelay,
		maxDelay:   maxRestartDelay,
		minHealthy: minHealthyRuntime,
		exitDelay:  exitWaitDelay,
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
		ran := time.Since(start)
		if healthyRun(ran, s.minHealthy) {
			// the process ran long enough to call it healthy, so don't carry a
			// backoff earned by an earlier, struggling run into this restart
			delay = s.minDelay
		}
		s.lg.Error("log stream stopped, restarting",
			log.KV("events", n), log.KV("runtime", ran.Round(time.Millisecond)),
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

// healthyRun reports whether a run that lasted ran counts as healthy, i.e. long
// enough that an exit shouldn't carry its restart backoff forward. A run that
// lasts exactly minHealthy counts as healthy.
func healthyRun(ran, minHealthy time.Duration) bool {
	return ran >= minHealthy
}

// runOnce runs a single log process to completion, returning the number of
// events ingested and why the process stopped.
func (s *streamer) runOnce(ctx context.Context) (n uint64, err error) {
	stderr := &stderrRelay{lg: s.lg}
	cmd := exec.CommandContext(ctx, logCommand, s.args...)
	cmd.Stderr = stderr
	cmd.WaitDelay = s.exitDelay
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
		t := time.AfterFunc(s.exitDelay, func() { s.killProcess(cmd) })
		werr = cmd.Wait()
		t.Stop()
	} else {
		s.killProcess(cmd)
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

// killProcess kills cmd's process, logging a warning if that fails for any
// reason other than the process already having exited.
func (s *streamer) killProcess(cmd *exec.Cmd) {
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		s.lg.Warn("failed to kill log process", log.KV("pid", cmd.Process.Pid), log.KVErr(err))
	}
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
	ent := &entry.Entry{
		TS:   ts,
		SRC:  s.src,
		Tag:  s.tag,
		Data: data,
	}
	if s.host != `` {
		ent.AddEnumeratedValues([]entry.EnumeratedValue{{Name: `host`, Value: entry.StringEnumData(s.host)}})
	}
	return ent
}

// stderrRelay forwards each line the log process writes to stderr into the ingester log,
// which is where messages such as an invalid predicate or missing privileges show up.
type stderrRelay struct {
	lg  *log.KVLogger
	buf []byte

	// midLine tracks whether we're mid-line, so later chunks aren't re-trimmed
	midLine bool
}

func (w *stderrRelay) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := w.buf[:i]
		w.buf = w.buf[i+1:]
		for len(line) > maxStderrLine {
			w.emit(line[:maxStderrLine], false)
			line = line[maxStderrLine:]
		}
		w.emit(line, true)
	}
	for len(w.buf) >= maxStderrLine {
		w.emit(w.buf[:maxStderrLine], false)
		w.buf = w.buf[maxStderrLine:]
	}
	w.buf = append([]byte(nil), w.buf...) // drop the consumed prefix
	return len(p), nil
}

func (w *stderrRelay) flush() {
	w.emit(w.buf, true)
	w.buf = nil
}

// emit logs chunk as one record; chunk must be at most maxStderrLine bytes.
// A long line gets split across several emit calls, with final set on the
// last one. Trimming only touches the line's outer edges, not every chunk,
// so whitespace sitting on a chunk boundary survives.
func (w *stderrRelay) emit(chunk []byte, final bool) {
	if !w.midLine {
		chunk = bytes.TrimLeftFunc(chunk, unicode.IsSpace)
	}
	w.midLine = !final
	if final {
		chunk = bytes.TrimRightFunc(chunk, unicode.IsSpace)
	}
	if len(chunk) > 0 {
		w.lg.Warn("log stream stderr", log.KV("output", string(chunk)))
	}
}
