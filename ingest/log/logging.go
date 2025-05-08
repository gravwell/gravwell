/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package log implements the Gravwell ingest and ingester logging systems
package log

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	stdliblog "log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/crewjam/rfc5424"
)

const (
	OFF      Level = 0
	DEBUG    Level = 1
	INFO     Level = 2
	WARN     Level = 3
	ERROR    Level = 4
	CRITICAL Level = 5
	FATAL    Level = 6
)

const (
	DEFAULT_DEPTH = 3

	DefaultID = `gw@1`

	maxProcID   = 128
	maxAppname  = 48
	maxHostname = 255
)

var (
	ErrNotOpen      = errors.New("Logger is not open")
	ErrInvalidLevel = errors.New("Log level is invalid")
)

type Level int

type metadata struct {
	hostname string
	appname  string
}

func (m *metadata) SetHostname(hostname string) error {
	if hostname != `` {
		if err := checkName(hostname); err != nil {
			return err
		}
	}
	if m.hostname = hostname; m.hostname == `` {
		//try to grab it via os package
		var lerr error
		if m.hostname, lerr = os.Hostname(); lerr == nil {
			if len(m.hostname) > maxHostname {
				m.hostname = m.hostname[0:maxHostname]
			}
		}
	}
	return nil
}

func (m *metadata) Appname() string {
	return m.appname
}

func (m *metadata) Hostname() string {
	return m.hostname
}

func (m *metadata) SetAppname(appname string) error {
	if appname != `` {
		if err := checkName(appname); err != nil {
			return err
		}
	}
	if m.appname = appname; len(m.appname) > maxAppname {
		m.appname = m.appname[0:maxAppname]
	}
	return nil
}

func (m *metadata) guessHostnameAppname() {
	m.SetHostname(``)
	if args := os.Args; len(args) > 0 {
		exe := filepath.Base(args[0])
		if ext := filepath.Ext(exe); len(ext) > 0 && len(ext) < len(exe) {
			exe = strings.TrimSuffix(exe, ext)
		}
		m.SetAppname(exe)
	}
}

type Relay interface {
	WriteLog(time.Time, []byte) error
}

type LevelRelay interface {
	WriteLog(Level, time.Time, string, string) error
	Close() error
}

type Logger struct {
	metadata
	wtrs []io.WriteCloser
	rls  []Relay
	lrls []LevelRelay
	mtx  sync.Mutex
	lvl  Level
	hot  bool
	raw  bool //output the old raw form rather than RFC5424
}

// NewFile creates a new logger with the first writer being a file
// The file is created if it does not exist, and is opened in append mode
// it is safe to use NewFile on existing logs
func NewFile(f string) (*Logger, error) {
	fout, err := os.OpenFile(f, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0660)
	if err != nil {
		return nil, err
	}
	return New(fout), nil
}

// New creates a new logger with the given writer at log level INFO
func New(wtr io.WriteCloser) (l *Logger) {
	l = &Logger{
		wtrs: []io.WriteCloser{wtr},
		mtx:  sync.Mutex{},
		lvl:  INFO,
		hot:  true,
	}
	l.guessHostnameAppname()
	return
}

// NewLevelRelay creates a new logger with just a level relay
func NewLevelRelay(lr LevelRelay) (l *Logger) {
	l = &Logger{
		lrls: []LevelRelay{lr},
		mtx:  sync.Mutex{},
		lvl:  INFO,
		hot:  true,
	}
	l.guessHostnameAppname()
	return
}

func NewDiscardLogger() *Logger {
	var dc discardCloser
	return New(dc)
}

// Close closes the logger and all currently associated writers
// writers that have been deleted are NOT closed
func (l *Logger) Close() (err error) {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	if err = l.ready(); err != nil {
		return
	}
	l.hot = false
	for i := range l.wtrs {
		if lerr := l.wtrs[i].Close(); lerr != nil {
			err = lerr
		}
	}
	for i := range l.lrls {
		if lerr := l.lrls[i].Close(); lerr != nil {
			err = lerr
		}
	}

	return
}

func (l *Logger) EnableRawMode() {
	l.raw = true //no need for a mutex here
}

func (l *Logger) RawMode() bool {
	return l.raw
}

func (l *Logger) ready() error {
	if !l.hot || (len(l.wtrs) == 0 && len(l.rls) == 0 && len(l.lrls) == 0) {
		return ErrNotOpen
	}
	return nil
}

// AddWriter will add a new writer which will get all the log lines as they are handled.
func (l *Logger) AddWriter(wtr io.WriteCloser) error {
	if wtr == nil {
		return errors.New("Invalid writer, is nil")
	}
	l.mtx.Lock()
	defer l.mtx.Unlock()
	if err := l.ready(); err != nil {
		return err
	}
	l.wtrs = append(l.wtrs, wtr)
	return nil
}

// AddRelay will add a new relay which will get all log entries as they are handled.
func (l *Logger) AddRelay(r Relay) error {
	if r == nil {
		return errors.New("Nil relay")
	}
	l.mtx.Lock()
	defer l.mtx.Unlock()
	if err := l.ready(); err != nil {
		return err
	}
	l.rls = append(l.rls, r)
	return nil
}

// AddLevelRelay will add a new relay which will get all log entries as they are handled.
func (l *Logger) AddLevelRelay(r LevelRelay) error {
	if r == nil {
		return errors.New("Nil relay")
	}
	l.mtx.Lock()
	defer l.mtx.Unlock()
	if err := l.ready(); err != nil {
		return err
	}
	l.lrls = append(l.lrls, r)
	return nil
}

// DeleteWriter removes a writer from the logger, it will not be closed on logging Close
func (l *Logger) DeleteWriter(wtr io.Writer) error {
	if wtr == nil {
		return errors.New("Invalid writer, is nil")
	}
	l.mtx.Lock()
	defer l.mtx.Unlock()
	if err := l.ready(); err != nil {
		return err
	}
	for i := len(l.wtrs) - 1; i >= 0; i-- {
		if l.wtrs[i] == wtr {
			l.wtrs = append(l.wtrs[:i], l.wtrs[i+1:]...)
		}
	}
	return nil
}

// DeleteRelay removes a relay from the logger.
func (l *Logger) DeleteRelay(rl Relay) error {
	if rl == nil {
		return errors.New("Nil relay")
	}
	l.mtx.Lock()
	defer l.mtx.Unlock()
	if err := l.ready(); err != nil {
		return err
	}
	for i := len(l.rls) - 1; i >= 0; i-- {
		if l.rls[i] == rl {
			l.rls = append(l.rls[:i], l.rls[i+1:]...)
		}
	}
	return nil
}

// SetLevelString sets the log level using a string, this is a helper function so that you can just hand
// the config file value directly in
func (l *Logger) SetLevelString(s string) error {
	lvl, err := LevelFromString(s)
	if err != nil {
		return err
	}
	return l.SetLevel(lvl)
}

// SetLevel sets the log level, Off disables logging and any logging call that is less than
// the level current level are not logged
func (l *Logger) SetLevel(lvl Level) error {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	if err := l.ready(); err != nil {
		return err
	}
	if !lvl.Valid() {
		return ErrInvalidLevel
	}
	l.lvl = lvl
	return nil
}

// GetLevel returns the current logging level
func (l *Logger) GetLevel() Level {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	if err := l.ready(); err != nil {
		return OFF
	}
	return l.lvl
}

// Debugf writes a DEBUG level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Debugf(f string, args ...interface{}) error {
	return l.outputf(DEFAULT_DEPTH, DEBUG, f, args...)
}

// Infof writes an INFO level log to the underlying writer using a format string,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Infof(f string, args ...interface{}) error {
	return l.outputf(DEFAULT_DEPTH, INFO, f, args...)
}

// Warnf writes an WARN level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Warnf(f string, args ...interface{}) error {
	return l.outputf(DEFAULT_DEPTH, WARN, f, args...)
}

// Errorf writes an ERROR level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Errorf(f string, args ...interface{}) error {
	return l.outputf(DEFAULT_DEPTH, ERROR, f, args...)
}

// Criticalf writes a CRITICALinfo level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Criticalf(f string, args ...interface{}) error {
	return l.outputf(DEFAULT_DEPTH, CRITICAL, f, args...)
}

// DebugfWithDepth writes a DEBUG level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) DebugfWithDepth(d int, f string, args ...interface{}) error {
	return l.outputf(d, DEBUG, f, args...)
}

// InfofWithDepth writes an INFO level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) InfofWithDepth(d int, f string, args ...interface{}) error {
	return l.outputf(d, INFO, f, args...)
}

// WarnfWithDepth writes an WARN level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) WarnfWithDepth(d int, f string, args ...interface{}) error {
	return l.outputf(d, WARN, f, args...)
}

// ErrorfWithDepth writes an ERROR level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) ErrorfWithDepth(d int, f string, args ...interface{}) error {
	return l.outputf(d, ERROR, f, args...)
}

// CriticalfWithDepthf writes a CRITICALinfo level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) CriticalfWithDepthf(d int, f string, args ...interface{}) error {
	return l.outputf(d, CRITICAL, f, args...)
}

// Fatalf writes a log, closes the logger, and issues an os.Exit(-1)
func (l *Logger) Fatalf(f string, args ...interface{}) {
	l.fatalfCode(DEFAULT_DEPTH, -1, f, args...)
}

// FatalfCode is identical to a log.Fatal, except it allows for controlling the exit code
func (l *Logger) FatalfCode(code int, f string, args ...interface{}) {
	l.fatalfCode(DEFAULT_DEPTH, code, f, args...)
}

// Debug writes a DEBUG level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Debug(msg string, sds ...rfc5424.SDParam) error {
	return l.outputStructured(DEFAULT_DEPTH, DEBUG, msg, sds...)
}

// Info writes an INFO level log to the underlying writer using a format string,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Info(msg string, sds ...rfc5424.SDParam) error {
	return l.outputStructured(DEFAULT_DEPTH, INFO, msg, sds...)
}

// Warn writes an WARN level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Warn(msg string, sds ...rfc5424.SDParam) error {
	return l.outputStructured(DEFAULT_DEPTH, WARN, msg, sds...)
}

// Error writes an ERROR level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Error(msg string, sds ...rfc5424.SDParam) error {
	return l.outputStructured(DEFAULT_DEPTH, ERROR, msg, sds...)
}

// Critical writes a CRITICALinfo level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Critical(msg string, sds ...rfc5424.SDParam) error {
	return l.outputStructured(DEFAULT_DEPTH, CRITICAL, msg, sds...)
}

// DebugWithDepth writes a DEBUG level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) DebugWithDepth(d int, msg string, sds ...rfc5424.SDParam) error {
	return l.outputStructured(d, DEBUG, msg, sds...)
}

// InfoWithDepth writes an INFO level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) InfoWithDepth(d int, msg string, sds ...rfc5424.SDParam) error {
	return l.outputStructured(d, INFO, msg, sds...)
}

// WarnWithDepth writes an WARN level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) WarnWithDepth(d int, msg string, sds ...rfc5424.SDParam) error {
	return l.outputStructured(d, WARN, msg, sds...)
}

// ErrorWithDepth writes an ERROR level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) ErrorWithDepth(d int, msg string, sds ...rfc5424.SDParam) error {
	return l.outputStructured(d, ERROR, msg, sds...)
}

// CriticalWithDepth writes a CRITICALinfo level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) CriticalWithDepth(d int, msg string, sds ...rfc5424.SDParam) error {
	return l.outputStructured(d, CRITICAL, msg, sds...)
}

// Fatal writes a log, closes the logger, and issues an os.Exit(-1)
func (l *Logger) Fatal(msg string, sds ...rfc5424.SDParam) {
	l.outputStructured(DEFAULT_DEPTH, FATAL, msg, sds...)
	os.Exit(-1)
}

// FatalCode is identical to a log.Fatal, except it allows for controlling the exit code
func (l *Logger) FatalCode(code int, msg string, sds ...rfc5424.SDParam) {
	l.outputStructured(DEFAULT_DEPTH, FATAL, msg, sds...)
	os.Exit(code)
}

func (l *Logger) fatalfCode(depth, code int, f string, args ...interface{}) {
	l.outputf(depth, FATAL, f, args...)
	os.Exit(code)
}

func (l *Logger) outputf(depth int, lvl Level, f string, args ...interface{}) (err error) {
	if l.lvl == OFF || lvl < l.lvl {
		return
	}
	ts := time.Now()
	ln, msg := l.genOutputf(ts, CallLoc(depth), lvl, f, args...)
	return l.writeOutput(lvl, ts, ln, msg)
}

func (l *Logger) outputStructured(depth int, lvl Level, msg string, sds ...rfc5424.SDParam) (err error) {
	if l.lvl == OFF || lvl < l.lvl {
		return
	}
	ts := time.Now()
	var ln string
	if l.raw {
		msg = renderMsgRaw(msg, sds...)
		ln = l.genRawOutput(ts, CallLoc(depth), lvl, msg)
	} else {
		ln, msg = l.genRfcOutput(ts, CallLoc(depth), lvl, msg, sds...)
	}
	return l.writeOutput(lvl, ts, ln, msg)
}

func (l *Logger) outputStructuredManualPtr(ptr uintptr, lvl Level, msg string, sds ...rfc5424.SDParam) (err error) {
	if l.lvl == OFF || lvl < l.lvl {
		return
	}
	ts := time.Now()
	var ln string
	if l.raw {
		msg = renderMsgRaw(msg, sds...)
		ln = l.genRawOutput(ts, CallLocFromPointer(ptr), lvl, msg)
	} else {
		ln, msg = l.genRfcOutput(ts, CallLocFromPointer(ptr), lvl, msg, sds...)
	}
	return l.writeOutput(lvl, ts, ln, msg)
}

func (l *Logger) writeOutput(lvl Level, ts time.Time, ln, msg string) (err error) {
	l.mtx.Lock()
	if err = l.ready(); err == nil {
		for _, w := range l.wtrs {
			if _, lerr := io.WriteString(w, ln); lerr != nil {
				err = lerr
			} else if _, lerr = io.WriteString(w, "\n"); lerr != nil {
				err = lerr
			}
		}
		for _, r := range l.rls {
			if lerr := r.WriteLog(ts, []byte(ln)); lerr != nil {
				err = lerr
			}
		}
		for _, r := range l.lrls {
			if lerr := r.WriteLog(lvl, ts, ln, msg); lerr != nil {
				err = lerr
			}
		}
	}
	l.mtx.Unlock()
	return
}

func (l *Logger) genOutputf(ts time.Time, pfx string, lvl Level, f string, args ...interface{}) (full, msg string) {
	msg = strings.TrimSpace(fmt.Sprintf(f, args...))
	if l.raw {
		full = l.genRawOutput(ts, pfx, lvl, msg)
	} else {
		full, _ = l.genRfcOutput(ts, pfx, lvl, msg)
	}
	return
}

func renderMsgRaw(msg string, sds ...rfc5424.SDParam) string {
	if len(sds) == 0 {
		return msg
	}
	var sb strings.Builder
	sb.WriteString(msg)
	//render it
	for i, p := range sds {
		if i == 0 && len(msg) == 0 {
			fmt.Fprintf(&sb, "%s=%q", p.Name, p.Value)
		} else {
			fmt.Fprintf(&sb, " %s=%q", p.Name, p.Value)
		}
	}
	return sb.String()
}

func (l *Logger) genRfcOutput(ts time.Time, pfx string, lvl Level, msg string, sds ...rfc5424.SDParam) (ln, rawmsg string) {
	rawmsg = renderMsgRaw(msg, sds...)
	if b, err := GenRFCMessage(ts, lvl.priority(), l.hostname, l.appname, pfx, msg, sds...); err == nil && len(b) > 0 {
		ln = string(bytes.Trim(b, "\n\r\t"))
	}
	return
}

func (l *Logger) StandardLogger() *stdliblog.Logger {
	var lvl slog.Level
	switch l.lvl {
	case OFF:
		lvl = -10 //basically off
	case DEBUG:
		lvl = slog.LevelDebug
	case INFO:
		lvl = slog.LevelInfo
	case WARN:
		lvl = slog.LevelWarn
	case ERROR:
		lvl = slog.LevelError
	case CRITICAL:
		lvl = slog.LevelError + 1
	case FATAL:
		lvl = slog.LevelError + 2
	}
	return slog.NewLogLogger(l, lvl)
}

// Enabled is a slog.Handler functions so that we can pass our Logger directly into a slog.NewLogLogger
func (l *Logger) Enabled(ctx context.Context, lvl slog.Level) bool {
	switch lvl {
	case slog.LevelDebug:
		return l.lvl == DEBUG
	case slog.LevelInfo:
		return (l.lvl > OFF && l.lvl <= INFO)
	case slog.LevelWarn:
		return (l.lvl > OFF && l.lvl <= WARN)
	case slog.LevelError:
		return (l.lvl > OFF && l.lvl <= ERROR)
	}
	return false
}

// WithGroup is a slog.Handler function doesn't really apply to our RFC5424 logger, so just return ourselves
func (l *Logger) WithGroup(name string) slog.Handler {
	return l
}

func convertAttr(attrs []slog.Attr) (sds []rfc5424.SDParam) {
	if len(attrs) > 0 {
		sds = make([]rfc5424.SDParam, len(attrs))
		for i, v := range attrs {
			sds[i] = rfc5424.SDParam{
				Name:  v.Key,
				Value: v.Value.String(),
			}
		}
	}
	return
}

func buildAttr(r slog.Record) (sds []rfc5424.SDParam) {
	if n := r.NumAttrs(); n > 0 {
		sds = make([]rfc5424.SDParam, 0, n)
		r.Attrs(func(a slog.Attr) bool {
			sds = append(sds, rfc5424.SDParam{Name: a.Key, Value: a.Value.String()})
			return true
		})
	}
	return
}

func (l *Logger) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) > 0 {
		return NewLoggerWithKV(l, convertAttr(attrs)...)
	}
	return l
}

func (l *Logger) Handle(ctx context.Context, r slog.Record) error {
	if !l.Enabled(ctx, r.Level) {
		return nil
	}
	switch r.Level {
	case slog.LevelDebug:
		return l.outputStructuredManualPtr(r.PC, DEBUG, r.Message, buildAttr(r)...)
	case slog.LevelInfo:
		return l.outputStructuredManualPtr(r.PC, INFO, r.Message, buildAttr(r)...)
	case slog.LevelWarn:
		return l.outputStructuredManualPtr(r.PC, WARN, r.Message, buildAttr(r)...)
	case slog.LevelError:
		return l.outputStructuredManualPtr(r.PC, ERROR, r.Message, buildAttr(r)...)
	}
	return nil
}

// GenRFCMessage formats the arguments into a proper RFC5424 message
// Per RFC5424 https://www.rfc-editor.org/rfc/rfc5424.html#section-6.2.7
//
// There are maximum lengths for some of the fields below:
//
//	AppName: 48
//	ProcID: 128
//	MsgID: 32
//	Hostname: 255
func GenRFCMessage(ts time.Time, prio rfc5424.Priority, hostname, appname, msgid, msg string, sds ...rfc5424.SDParam) ([]byte, error) {
	m := rfc5424.Message{
		Priority:  prio,
		Timestamp: ts,
		Hostname:  trimLength(255, hostname),
		AppName:   trimLength(48, appname),
		MessageID: trimPathLength(32, msgid),
		Message:   []byte(msg),
	}
	if len(sds) > 0 {
		m.StructuredData = []rfc5424.StructuredData{
			rfc5424.StructuredData{
				ID:         DefaultID,
				Parameters: sds,
			},
		}
	}
	return m.MarshalBinary()
}

func (l *Logger) genRawOutput(ts time.Time, pfx string, lvl Level, msg string) (ln string) {
	ln = ts.UTC().Format(time.RFC3339) + " " + pfx + " " + lvl.String() + " " + msg
	return
}

// Write implements a writer interface so it can be handed to a standard logger
func (l *Logger) Write(b []byte) (n int, err error) {
	l.mtx.Lock()
	if err = l.ready(); err == nil {
		n = len(b)
		for _, w := range l.wtrs {
			if _, lerr := w.Write(b); lerr != nil {
				err = lerr
			}
		}
	}

	l.mtx.Unlock()
	return
}

func (l Level) String() string {
	switch l {
	case OFF:
		return `OFF`
	case DEBUG:
		return `DEBUG`
	case INFO:
		return `INFO`
	case WARN:
		return `WARN`
	case ERROR:
		return `ERROR`
	case CRITICAL:
		return `CRITICAL`
	case FATAL:
		return `FATAL`
	}
	return `UNKNOWN`
}

func (l Level) Valid() bool {
	switch l {
	case OFF:
		fallthrough
	case DEBUG:
		fallthrough
	case INFO:
		fallthrough
	case WARN:
		fallthrough
	case ERROR:
		fallthrough
	case CRITICAL:
		fallthrough
	case FATAL:
		return true
	}
	return false
}

func (l Level) priority() rfc5424.Priority {
	switch l {
	case OFF:
		return 0
	case DEBUG:
		return rfc5424.User | rfc5424.Debug
	case INFO:
		return rfc5424.User | rfc5424.Info
	case WARN:
		return rfc5424.User | rfc5424.Warning
	case ERROR:
		return rfc5424.User | rfc5424.Error
	case CRITICAL:
		return rfc5424.User | rfc5424.Crit
	case FATAL:
		return rfc5424.User | rfc5424.Emergency
	}
	return rfc5424.User | rfc5424.Debug
}

func LevelFromString(s string) (l Level, err error) {
	s = strings.ToUpper(s)
	switch s {
	case `OFF`:
		l = OFF
	case `DEBUG`:
		l = DEBUG
	case `INFO`:
		l = INFO
	case `WARN`:
		l = WARN
	case `ERROR`:
		l = ERROR
	case `CRITICAL`:
		l = CRITICAL
	case `FATAL`:
		l = FATAL
	default:
		err = ErrInvalidLevel
	}
	return
}

type discardCloser bool

func (dc discardCloser) Write(b []byte) (int, error) {
	return len(b), nil
}

func (dc discardCloser) Close() error {
	return nil
}

// we have a separate func for error so the call depths are always consistent
// prefix attaches the timestamp and filepath to the log entry
// the lvl indicates how far up the caller stack we need to go
func prefix(callDepth int) (s string) {
	ts := time.Now().UTC().Format(time.RFC3339)
	//get the file and line that caused the error
	if _, file, line, ok := runtime.Caller(callDepth); ok {
		dir, file := filepath.Split(file)
		file = filepath.Join(filepath.Base(dir), file)
		s = fmt.Sprintf("%s %s:%d", ts, file, line)
	}
	return
}

func CallLoc(callDepth int) (s string) {
	//get the file and line that caused the error
	if _, file, line, ok := runtime.Caller(callDepth); ok {
		dir, file := filepath.Split(file)
		file = filepath.Join(filepath.Base(dir), file)
		s = fmt.Sprintf("%s:%d", file, line)
	}
	return
}

func CallLocFromPointer(ptr uintptr) (s string) {
	if ptr == 0 {
		return
	}
	if frames := runtime.CallersFrames([]uintptr{ptr}); frames != nil {
		if frame, ok := frames.Next(); ok {
			s = fmt.Sprintf("%s:%d", frame.File, frame.Line)
		}
	}
	return
}

func NewStderrLogger(fileOverride string) (*Logger, error) {
	return newStderrLogger(fileOverride, nil)
}

func NewStderrLoggerEx(fileOverride string, cb StderrCallback) (*Logger, error) {
	return newStderrLogger(fileOverride, cb)
}

type StderrCallback func(io.Writer)

func checkName(v string) (err error) {
	//check that bits[0] only contains ascii values
	for _, r := range v {
		//must be a-z or A-Z, or . _, -
		if r >= 'a' && r <= 'z' {
			continue
		} else if r >= 'A' && r <= 'Z' {
			continue
		} else if r >= '0' && r <= '9' {
			continue
		} else if r == '.' || r == '_' || r == '-' || r == ':' {
			continue
		}
		err = fmt.Errorf("name character %c is invalid", r)
		return
	}
	return
}

// trimPathLength will trim the input path to no more than the last i bytes of
// the basename of input. For example, "KafkaFederator/kafkaWriter.go:352" will
// be trimmed to "kafkaWriter.go:352"
func trimPathLength(i int, input string) string {
	if len(input) <= i {
		return input
	}
	base := filepath.Base(input)

	if len(base) <= i {
		return base
	}

	return base[len(base)-32:]
}

func trimLength(i int, input string) string {
	if len(input) <= i {
		return input
	}
	return input[:i]
}
