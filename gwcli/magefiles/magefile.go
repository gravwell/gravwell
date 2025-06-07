//go:build mage

/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
The build system for gwcli, built on Mage.
Because it is self-contained, you can also just use go build inside of the gwcli directory
(or go build -C gwcli from the top-level gravwell directory.)
The Magefile serves mostly to corral the testing into a single location.

You can use the envvar MAGEFILE_ENABLE_COLOR if you want pretty colors.
*/
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const (
	_BINARY_TARGET string = "gwcli"
)

var (
	green = "\u001b[32m"
	red   = "\u001b[31m"
	reset = "\u001b[0m"
)

//#region helper functions

// Only prints the given string if verbose mode is enabled.
func verboseln(s string) {
	if mg.Verbose() {
		fmt.Println(s)
	}
}

// Colors the text in green.
func good(txt string) string {
	return green + txt + reset
}

// Colors the text in red.
func bad(txt string) string {
	return red + txt + reset
}

// Colors the text in yellow.
func mid(txt string) string {
	return red + txt + reset
}

// Runs the given test and outputs (verbose-dependent) its error log (or "ok").
// If testPattern is empty, runs all tests found in testPath (omitting "-run").
// Returns the error that occurred (if applicable).
func runTest(timeout time.Duration, testPattern, testPath string) error {
	var cmd *exec.Cmd
	if testPattern == "" {
		cmd = exec.Command("go", "test", "-race", "-v", "-timeout", timeout.String(), testPath)
	} else {
		cmd = exec.Command("go", "test", "-race", "-v", "-timeout", timeout.String(), "-run", testPattern, testPath)
	}
	verboseln(cmd.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("%s", out)
		return err
	}
	return nil
}

//#endregion

//#region setup

func init() {
	// if color has been disabled, set all of the color prefixes (and reset suffix) to the empty string
	if !mg.EnableColor() {
		green = ""
		reset = ""
	}
}

//#endregion

// Default target to run when none is specified
// If not set, running mage will list available targets
//var Default = Build

// Build compiles gwcli for your local architecture and outputs it to pwd.
func Build() error {
	pwd, err := os.Getwd()
	if err != nil {
		verboseln(fmt.Sprintf("failed to get pwd: %s. Defaulting to local directory.", err))
		pwd = "."
	}

	output := path.Join(pwd, _BINARY_TARGET)
	verboseln("Building " + output + "...")
	cmd := exec.Command("go", "build", "-o", output, ".")
	return cmd.Run()
}

func Vet() error {
	var display = func(txt string, err error, output string) {
		if err != nil {
			fmt.Println(bad(txt), "\n", output)
		} else if output != "" {
			fmt.Println(mid(txt), "\n", output)
		} else {
			fmt.Println(good(txt), "ok")
		}
	}

	vetOut, vetErr := sh.Output("go", "vet", "./...")
	display("go vet", vetErr, vetOut)
	scOut, scErr := sh.Output("staticcheck", "./...")
	display("staticcheck", scErr, scOut)
	return errors.Join(vetErr, scErr)

}

// TestAll runs all gwcli tests via `./...` expansion.
// If run with -v or an error occurs, prints outcome to stdout.
func TestAll(cover, noCache bool) error {
	args := []string{"test", "-race", "-vet=all"}
	if cover {
		args = append(args, "-cover")
	}
	if noCache {
		args = append(args, "-count=1")
	}

	args = append(args, "./...")

	out, err := sh.Output("go", args...)
	if mg.Verbose() || err != nil {
		fmt.Println(out)
	}
	return err
}

// TestIntegration calls the tests in script_test for targeting external, automated usage (via --script).
func TestIntegration() error {
	coverdirPath := path.Join(os.TempDir(), "coverout")
	if err := os.Mkdir(coverdirPath, 0660); err != nil {
		return err
	}

	v := ""
	if mg.Verbose() {
		v = "-v"
	}

	// build a cover-instrumented binary
	out, err := sh.OutputWith(map[string]string{"GOCOVERDIR": coverdirPath},
		"go", "build", v, "-cover", "-o=test_gwcli", ".")
	if mg.Verbose() || err != nil {
		fmt.Println(out)
		if err != nil {
			return err
		}
	}

	// run integration tests external to the binary
	fmt.Println("NYI")

	// spit out coverage data
	out, err = sh.Output("go", "tool", "covdata", "percent", "-i="+coverdirPath)
	fmt.Println(out)
	if err != nil {
		return err
	}

	return nil
}

// Clean up the binary and any and all logs.
// Does not destroy login token.
//
// Running with dryrun prints out what files would be deleted, but does not actually delete them.
// You probably want to run it with -v.
//
// If an error occurs, it will immediately stop processing if !dryrun.
func Clean(dryrun bool) (err error) {
	// Destroy the binary
	binPath := path.Join(".", _BINARY_TARGET)
	if err := dryRM(binPath, dryrun); err != nil {
		return err
	}

	// Destroy log files in the config directory
	if err := dryRM(cfgdir.DefaultStdLogPath, dryrun); err != nil {
		return err
	}
	if err := dryRM(cfgdir.DefaultRestLogPath, dryrun); err != nil {
		return err
	}

	return nil
}

// Deletes or faux-deletes the given path according to dry run, verbose-printing the result.
// Returns errors if they occur while !dryrun
func dryRM(path string, dryrun bool) error {
	const _DRYRUN_PREFIX string = "DRYRUN: "
	var result string
	if dryrun {
		if _, err := os.Stat(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			result = _DRYRUN_PREFIX + "failed to remove file: " + err.Error()
		} else if errors.Is(err, fs.ErrNotExist) {
			// do nothing
		} else {
			result = _DRYRUN_PREFIX + path + " would have been deleted"
		}
	} else {
		if err := os.Remove(path); err == nil {
			result = "Deleted " + path

		} else if errors.Is(err, fs.ErrNotExist) {
			// do nothing, file doesn't exist
		} else {
			return fmt.Errorf("failed to remove file: %v", err)
		}
	}

	if result != "" {
		verboseln(result)
	}
	return nil
}
