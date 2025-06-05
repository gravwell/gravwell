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
)

const (
	_BINARY_TARGET string = "gwcli"
)

var (
	green = "\u001b[32m"
	reset = "\u001b[0m"
)

//#region helper functions

// Only prints the given string if verbose mode is enabled.
func verboseln(s string) {
	if mg.Verbose() {
		fmt.Println(s)
	}
}

// Prints out "ok" iff verbose mode is enabled.
func ok() {
	verboseln(green + "ok" + reset)
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
	ok()
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

// TestAll runs all gwcli tests, according to their subsystem.
func TestAll() error {
	verboseln("Testing non-Mother singletons...")
	mg.Deps(TestConnection)

	verboseln("Testing query components...")
	mg.Deps(TestQuery, TestDatascope, TestQueryAux)

	verboseln("Testing utilities...")
	mg.Deps(TestScaffold)

	verboseln("Testing Mother...")
	mg.Deps(TestMotherHistory, TestMotherMode, TestMotherMisc)

	verboseln("Testing direct usage via --script...")
	return TestScript()
}

// TestScript calls the tests in script_test for targeting external, automated usage (via --script).
func TestScript() error {
	if err := runTest(10*time.Second, "^TestMacros$", "github.com/gravwell/gravwell/v4/gwcli"); err != nil {
		return err
	}
	if err := runTest(3*time.Minute, "^TestQueries$", "github.com/gravwell/gravwell/v4/gwcli"); err != nil {
		return err
	}

	return nil
}

// TestQuery tests executing queries via gwcli.
func TestQuery() error {
	if err := runTest(30*time.Second, "^Test_tryQuery$", "github.com/gravwell/gravwell/v4/gwcli/tree/query"); err != nil {
		return err
	}
	if err := runTest(20*time.Second, "^Test_run$", "github.com/gravwell/gravwell/v4/gwcli/tree/query"); err != nil {
		return err
	}

	return nil
}

// TestConnection tests the connection package, primarily the Login functionality contained therein.
func TestConnection() error {
	// run the script mode tests
	if err := runTest(30*time.Second, "_script_mode$", "github.com/gravwell/gravwell/v4/gwcli/connection"); err != nil {
		return err
	}
	// run the interactive mode tests with tight timeouts
	if err := runTest(8*time.Second, "^TestLogin_interactive_mode$", "github.com/gravwell/gravwell/v4/gwcli/tree/connection"); err != nil {
		return err
	}

	return nil
}

// TestDatascope tests the Datascope query subsystem.
func TestDatascope() error {
	const _TIMEOUT time.Duration = 4 * time.Minute
	if err := runTest(_TIMEOUT, "^TestKeepAlive$", "github.com/gravwell/gravwell/v4/gwcli/tree/query/datascope"); err != nil {
		return err
	}

	return nil
}

// TestQueryAux tests the query-supporting functions.
func TestQueryAux() error {
	const _TIMEOUT time.Duration = 30 * time.Second
	if err := runTest(_TIMEOUT, "", "github.com/gravwell/gravwell/v4/gwcli/utilities/querysupport"); err != nil {
		return err
	}
	return nil
}

// TestScaffold tests the scaffold builder functions.
func TestScaffold() error {
	const _TIMEOUT time.Duration = 30 * time.Second
	if err := runTest(_TIMEOUT, "^Test_format_String$", "github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"); err != nil {
		return err
	}

	return nil
}

// TestMotherHistory tests Mother's history system.
func TestMotherHistory() error {
	const _TIMEOUT time.Duration = 30 * time.Second
	if err := runTest(_TIMEOUT, "",
		"github.com/gravwell/gravwell/v4/gwcli/mother"); err != nil {
		return err
	}

	return nil
}

// TestMotherMode runs tests for mother's mode handling.
func TestMotherMode() error {
	const _TIMEOUT time.Duration = 30 * time.Second
	if err := runTest(_TIMEOUT, "",
		"github.com/gravwell/gravwell/v4/gwcli/mother"); err != nil {
		return err
	}

	return nil
}

// TestMotherMisc runs tests for Mother that are not otherwise sub-divided.
func TestMotherMisc() error {
	const _TIMEOUT time.Duration = 30 * time.Second
	if err := runTest(_TIMEOUT, "",
		"github.com/gravwell/gravwell/v4/gwcli/mother"); err != nil {
		return err
	}

	return nil
}

// A custom install step if you need your bin someplace other than go/bin
/*func Install() error {
	mg.Deps(Build)
	fmt.Println("Installing...")
	// check that we are root prior to moving
	return os.Rename("./gwcli", "/bin/gwcli")
} */

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
