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
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/gwcli/internal/state"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/log/rotate"
	"github.com/spf13/pflag"
)

//#region errors

var ErrEmptyPath error = errors.New("path cannot be empty")

type ErrInternal struct{}

func (ErrInternal) Error() string {
	return "an internal error occurred; please submit an issue and include " + cfgdir.DefaultStdLogPath
}

//#endregion errors

const (
	mb                = 1024 * 1024
	maxLogSize  int64 = 10 * mb
	maxLogCount uint  = 8
)

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

// ! These flags were pulled out of flagtext to avoid import cycles.

// FlagLogPath contains details of the flag used to define where clilog logs to.
var FlagLogPath struct {
	Name         string
	Shorthand    string
	DefaultValue string
	Description  string
} = struct {
	Name         string
	Shorthand    string
	DefaultValue string
	Description  string
}{"log", "l", cfgdir.DefaultStdLogPath, "log location for developer logs"}

// FlagLogLevel contains details of the flag used to set the verbosity of clilog.
var FlagLogLevel struct {
	Name         string
	DefaultValue string
	Description  string
} = struct {
	Name         string
	DefaultValue string
	Description  string
}{"loglevel", "INFO", "log level for developer logs (-l).\n" +
	"Possible values: 'OFF', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'CRITICAL', 'FATAL'.\n" +
	"NOTE: DEBUG mode may enable additional validation checks and may have a minor performance impact."}

// Writer is the logging singleton.
var Writer *log.Logger

// InitializeFromArgs parses out --log and --loglevel from a set of arguments, ignoring any and all other flags.
// This enables clilogger to be brought online and configured before any other handling is prepared.
//
// If args is nil or an error occurs, the logger will be initialized with its defaults.
//
// Safe to call multiple times; subsequent calls will be no-ops.
func InitializeFromArgs(args []string) {
	if Writer != nil {
		return
	}
	// args may include flags unrelated to the logger; ignore them
	logFlags := pflag.NewFlagSet("logging", pflag.PanicOnError)
	logFlags.StringP(FlagLogPath.Name, FlagLogPath.Shorthand, FlagLogPath.DefaultValue, FlagLogPath.Description)
	logFlags.String(FlagLogLevel.Name, FlagLogLevel.DefaultValue, FlagLogLevel.Description)

	logFlags.BoolP("help", "h", false, "") // re-define the help flag

	logFlags.ParseErrorsWhitelist = pflag.ParseErrorsWhitelist{UnknownFlags: true}
	if err := logFlags.Parse(args); err != nil {
		panic(err) // if this pops, something has gone horribly wrong and we need to know
	}

	path, err := logFlags.GetString(FlagLogPath.Name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to get log path flag to initialize clilog: ", err)
	}
	lvl, err := logFlags.GetString(FlagLogLevel.Name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to get log level flag to initialize clilog: ", err)
	}
	if err = Init(path, lvl); err != nil {
		// try again with the defaults
		if secondErr := Init(cfgdir.DefaultStdLogPath, lvl); secondErr != nil {
			// if this happens, something is VERY wrong. Install a nil logger to allow gwcli to continue to limp along
			fmt.Fprintf(os.Stderr, "failed to generate a logger:\n1) %v\n2) %v\n", err, secondErr)
			Writer = log.NewDiscardLogger()
		}
		// log the original error
		Writer.Error("failed to initialize logger with given parameters", log.KVErr(err))
	}

}

// Init initializes Writer, the logging singleton.
//
// Safe to call multiple times; subsequent calls will be no-ops.
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
	if lvl == log.DEBUG {
		state.SetDebugMode()
	}

	// spawn a log rotator on the given file
	lr, err := rotate.OpenEx(path, 0660, maxLogSize, maxLogCount, true)
	if err != nil {
		return err
	}
	// spawn a logger on our logrotator
	Writer = log.New(lr)
	if err = Writer.SetLevel(lvl); err != nil {
		Writer.Close()
		return err
	}

	// empty out the extra info gwcli does not benefit from
	Writer.SetAppname(".")
	Writer.SetHostname(".") // autopopulates if empty

	// error check the first call
	if err := Writer.Infof("\n\n--- Logger initialized at %v level ---", Writer.GetLevel()); err != nil {
		Writer.Close()
		return err
	}

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
	if alt != nil {
		alt.Write([]byte(str))
	}
	if Writer == nil {
		return
	}
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
	if Writer == nil {
		return false
	}
	return Writer.GetLevel() <= log.Level(lvl)
}

//#region helpers

// GetFlag logs a warning that we failed to get an expected flag out of a flagset.
//
// This error is almost certainly developer error.
//
// Returns ErrInternal, which the caller may return if this failure is fatal.
// It is safe to ignore the return value.
func GetFlag(err error) ErrInternal {
	if err == nil {
		return ErrInternal{}
	}
	if Writer != nil {
		// TODO test call depth
		Writer.Warn("flag-get failure", log.KV("parent", log.CallLoc(1)), log.KVErr(err))
	}
	return ErrInternal{}
}

// CloseFile closes the give file and logs the error if one occurred.
func CloseFile(f *os.File) {
	if err := f.Close(); err != nil {
		Writer.Warn("failed to close file", log.KV("parent", log.CallLoc(1)), log.KVErr(err))
	}

}

// TypeAssert logs an error that an assertion failed.
// Most commonly used when asserting list.Item to a local, enriched type.
//
// Returns ErrInternal, which the caller may return if this failure is fatal.
// It is safe to ignore the return value.
func TypeAssert(baseItem any, targetType any) ErrInternal {
	if Writer != nil {
		Writer.Critical("type assert failure",
			log.KV("parent", log.CallLoc(1)),
			log.KV("base item", baseItem),
			log.KV("target item", targetType))
	}

	return ErrInternal{}
}

// ProgramOptions returns loggable entries of what the given input and output are.
func ProgramOptions(in io.Reader, out io.Writer) rfc5424.SDParam {
	opts := []string{}
	var value = "input->"
	if in == os.Stdin {
		value += "stdin"
	} else {
		value += fmt.Sprintf("%p", in)
	}
	opts = append(opts, value)
	value = "output->"
	if out == os.Stdout {
		value += "stdout"
	} else {
		value += fmt.Sprintf("%p", in)
	}
	opts = append(opts, value)

	return log.KV("ProgramOptions", opts)
}
