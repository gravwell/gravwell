/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package clilog provides the logger for gwcli in the form of a logging singleton: Writer.

It is basically a singleton wrapper of the gravwell ingest logger.
While the underlying ingest logger appears to be thread-safe, clilog's helper functions are not
necessarily.
*/
package clilog

import (
	"errors"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

//#region errors

var ErrEmptyPath error = errors.New("path cannot be empty")

//#endregion errors

// Level recreates log.Level so other packages do not have to import the ingest logger
type Level int

const (
	OFF      Level = 0
	DEBUG    Level = 1
	INFO     Level = 2
	WARN     Level = 3
	ERROR    Level = 4
	CRITICAL Level = 5
	FATAL    Level = 6
)

// Writer is the logging singleton.
var Writer *log.Logger

// Init initializes Writer, the logging singleton.
// Safe (ineffectual) if the writer has already been initialized.
func Init(path string, lvlString string) error {
	var err error
	if Writer != nil {
		return nil
	}

	// validate parameters
	if path = strings.TrimSpace(path); path == "" {
		return ErrEmptyPath
	}
	lvl, err := log.LevelFromString(lvlString)
	if err != nil {
		return err
	}

	Writer, err = log.NewFile(path)
	if err != nil {
		if Writer != nil {
			errors.Join(err, Writer.Close())
		}
		return err
	}

	if err = Writer.SetLevel(lvl); err != nil {
		Writer.Close()
		return err
	}

	Writer.Infof("Logger initialized at %v level, hostname %v", Writer.GetLevel(), Writer.Hostname())

	Writer.SetAppname(".")
	Writer.SetHostname(".") // autopopulates if empty

	return nil
}

// Destroy closes the writer's file and nils out the Writer.
func Destroy() error {
	if Writer == nil {
		return nil
	}
	err := Writer.Close()
	Writer = nil
	return err
}

// Tee writes the error to clilog.Writer and a secondary output, usually stderr
func Tee(lvl Level, alt io.Writer, str string) {
	alt.Write([]byte(str))
	switch lvl {
	case OFF:
	case DEBUG:
		Writer.Debug(str)
	case INFO:
		Writer.Info(str)
	case WARN:
		Writer.Warn(str)
	case ERROR:
		Writer.Error(str)
	case CRITICAL:
		Writer.Critical(str)
	case FATAL:
		Writer.Fatal(str)
	}
}

// Active returns whether or not the given level is currently enabled (<= log.Level)
func Active(lvl Level) bool {
	return Writer.GetLevel() <= log.Level(lvl)
}

// LogFlagFailedGet logs the non-fatal failure to fetch named flag from flagset.
// Used to keep flag handling errors uniform.
func LogFlagFailedGet(flagname string, err error) {
	Writer.Warnf("failed to fetch '--%v':%v\nignoring", flagname, err)
}

var dbgMsgSty = lipgloss.NewStyle().Italic(true)

// LogMsg is a helper method for consistently displaying messages (at the debug level).
func LogMsg(str string, msg tea.Msg) {
	Writer.Debugf("%s\n\t"+dbgMsgSty.Render("%#v"), str, msg)
}
