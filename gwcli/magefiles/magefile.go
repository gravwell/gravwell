/*
The build system for gwcli, built on Mage.
Because it is self-contained, you can also just use go build inside of the gwcli directory
(or go build -C gwcli from the top-level gravwell directory.)
The Magefile serves mostly to corral the testing into a single location.
*/
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"time"

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
// Returns the error that occurred (if applicable).
func runTest(timeout time.Duration, testPattern, testPath string) error {
	cmd := exec.Command("go", "test", "-v", "-timeout", timeout.String(), "-run", testPattern, testPath)
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

// Compiles gwcli for your local architecture and outputs it to pwd.
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

// Runs all gwcli tests.
func TestAll() error {
	verboseln("Testing query components...")
	mg.Deps(TestQuery, TestDatascope)

	mg.Deps(TestMain)
	return nil
}

// Runs the top-level tests that mimic scripted user input.
func TestMain() error {
	const _TIMEOUT time.Duration = 25 * time.Second
	if err := runTest(_TIMEOUT, "^TestNonInteractive$", "github.com/gravwell/gravwell/v4/gwcli"); err != nil {
		return err
	}
	if err := runTest(_TIMEOUT, "^TestNonInteractiveQueryFileOut$", "github.com/gravwell/gravwell/v4/gwcli"); err != nil {
		return err
	}
	return nil
}

// Tests the Query system.
func TestQuery() error {
	const _TIMEOUT time.Duration = 20 * time.Second
	if err := runTest(_TIMEOUT, "^Test_tryQuery$", "github.com/gravwell/gravwell/v4/gwcli/tree/query"); err != nil {
		return err
	}

	if err := runTest(_TIMEOUT, "^Test_run$", "github.com/gravwell/gravwell/v4/gwcli/tree/query"); err != nil {
		return err
	}

	return nil
}

// Tests the Datascope query subsystem.
func TestDatascope() error {
	const _TIMEOUT time.Duration = 4 * time.Minute
	if err := runTest(_TIMEOUT, "^TestKeepAlive$", "github.com/gravwell/gravwell/v4/gwcli/tree/query/datascope"); err != nil {
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

// Clean up the binary and any and all logs
func Clean() {
	verboseln("Cleaning...")
	os.RemoveAll(path.Join(".", _BINARY_TARGET))
	// TODO remove log files
}
