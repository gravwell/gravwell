/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package log

import (
	"errors"
	"fmt"
	"io"
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

	defaultID = `gw@1`

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

type Logger struct {
	metadata
	wtrs []io.WriteCloser
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
	return
}

func (l *Logger) EnableRawMode() {
	l.raw = true //no need for a mutex here
}

func (l *Logger) RawMode() bool {
	return l.raw
}

func (l *Logger) ready() error {
	if !l.hot || len(l.wtrs) == 0 {
		return ErrNotOpen
	}
	return nil
}

// Add a new writer which will get all the log lines as they are handled
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

// Debug writes a DEBUG level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Debug(f string, args ...interface{}) error {
	return l.output(DEFAULT_DEPTH, DEBUG, f, args...)
}

// Info writes an INFO level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Info(f string, args ...interface{}) error {
	return l.output(DEFAULT_DEPTH, INFO, f, args...)
}

// Warn writes an WARN level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Warn(f string, args ...interface{}) error {
	return l.output(DEFAULT_DEPTH, WARN, f, args...)
}

// Error writes an ERROR level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Error(f string, args ...interface{}) error {
	return l.output(DEFAULT_DEPTH, ERROR, f, args...)
}

// Critical writes a CRITICALinfo level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) Critical(f string, args ...interface{}) error {
	return l.output(DEFAULT_DEPTH, CRITICAL, f, args...)
}

// Debug writes a DEBUG level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) DebugWithDepth(d int, f string, args ...interface{}) error {
	return l.output(d, DEBUG, f, args...)
}

// Info writes an INFO level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) InfoWithDepth(d int, f string, args ...interface{}) error {
	return l.output(d, INFO, f, args...)
}

// Warn writes an WARN level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) WarnWithDepth(d int, f string, args ...interface{}) error {
	return l.output(d, WARN, f, args...)
}

// Error writes an ERROR level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) ErrorWithDepth(d int, f string, args ...interface{}) error {
	return l.output(d, ERROR, f, args...)
}

// Critical writes a CRITICALinfo level log to the underlying writer,
// if the logging level is higher than DEBUG no action is taken
func (l *Logger) CriticalWithDepth(d int, f string, args ...interface{}) error {
	return l.output(d, CRITICAL, f, args...)
}

// Fatal writes a log, closes the logger, and issues an os.Exit(-1)
func (l *Logger) Fatal(f string, args ...interface{}) {
	l.fatalCode(DEFAULT_DEPTH, -1, f, args...)
}

// FatalCode is identical to a log.Fatal, except it allows for controlling the exit code
func (l *Logger) FatalCode(code int, f string, args ...interface{}) {
	l.fatalCode(DEFAULT_DEPTH, code, f, args...)
}

func (l *Logger) fatalCode(depth, code int, f string, args ...interface{}) {
	var ln string
	var nlReq bool
	if l.raw {
		ln = prefix(depth) + " FATAL " + fmt.Sprintf(f, args...)
	} else {
		ln = l.genRfcOutput(callLoc(depth), FATAL, fmt.Sprintf(f, args...))
	}
	if !strings.HasPrefix(ln, "\n") {
		nlReq = true
	}
	l.mtx.Lock()
	for _, w := range l.wtrs {
		io.WriteString(w, ln)
		if nlReq {
			io.WriteString(w, "\n")
		}
		w.Close()
	}
	os.Exit(code)
	l.mtx.Unlock() //won't ever happen, but leave it so that changes later don't cause mutex problems
}

func (l *Logger) output(depth int, lvl Level, f string, args ...interface{}) (err error) {
	var nlReq bool
	l.mtx.Lock()
	if err = l.ready(); err == nil && l.lvl <= lvl && l.lvl != OFF {
		ln := l.genOutput(callLoc(depth), lvl, f, args...)
		if !strings.HasPrefix(ln, "\n") {
			nlReq = true
		}
		for _, w := range l.wtrs {
			if _, lerr := io.WriteString(w, ln); lerr != nil {
				err = lerr
			} else if nlReq {
				if _, lerr = io.WriteString(w, "\n"); lerr != nil {
					err = lerr
				}
			}
		}
	}
	l.mtx.Unlock()
	return
}

func (l *Logger) genOutput(pfx string, lvl Level, f string, args ...interface{}) (ln string) {
	if l.raw {
		return l.genRawOutput(pfx, lvl, f, args...)
	}
	return l.genRfcOutput(pfx, lvl, fmt.Sprintf(f, args...))
}

func (l *Logger) genRfcOutput(pfx string, lvl Level, payload string) (ln string) {
	m := rfc5424.Message{
		Priority:  lvl.priority(),
		Timestamp: time.Now(),
		Hostname:  l.hostname,
		AppName:   l.appname,
		MessageID: pfx,
		Message:   []byte(payload),
	}
	if b, err := m.MarshalBinary(); err == nil {
		ln = string(b)
	}
	return
}

func (l *Logger) genRawOutput(pfx string, lvl Level, f string, args ...interface{}) string {
	var nl string
	if !strings.HasSuffix(f, "\n") {
		nl = "\n"
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	return ts + " " + pfx + " " + lvl.String() + " " + fmt.Sprintf(f, args...) + nl
}

// implement writer interface so it can be handed to a standard loger
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
		return rfc5424.User + rfc5424.Debug
	case INFO:
		return rfc5424.User + rfc5424.Info
	case WARN:
		return rfc5424.User + rfc5424.Warning
	case ERROR:
		return rfc5424.User + rfc5424.Error
	case CRITICAL:
		return rfc5424.User + rfc5424.Crit
	case FATAL:
		return rfc5424.User + rfc5424.Emergency
	}
	return rfc5424.User + rfc5424.Debug
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

//we have a separate func for error so the call depths are always consistent
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

func callLoc(callDepth int) (s string) {
	//get the file and line that caused the error
	if _, file, line, ok := runtime.Caller(callDepth); ok {
		dir, file := filepath.Split(file)
		file = filepath.Join(filepath.Base(dir), file)
		s = fmt.Sprintf("%s:%d", file, line)
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
		} else if r == '.' || r == '_' || r == '-' || r == ':' {
			continue
		}
		err = fmt.Errorf("name character %c is invalid", r)
		return
	}
	return
}
