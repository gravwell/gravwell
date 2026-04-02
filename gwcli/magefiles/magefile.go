/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
The build system for gwcli, built on Mage.
Manages testing, code generation, and (obviously) compilation.

You can use the envvar MAGEFILE_ENABLE_COLOR if you want pretty colors.
*/
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const (
	_BINARY_TARGET string = "gwcli"
)

var (
	green  = "\u001b[32m"
	red    = "\u001b[31m"
	yellow = "\u001b[33m"
	reset  = "\u001b[0m"
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
	return yellow + txt + reset
}

//#endregion

//#region setup

func init() {
	// if color has been disabled, set all of the color prefixes (and reset suffix) to the empty string
	if !mg.EnableColor() {
		green = ""
		red = ""
		yellow = ""
		reset = ""
	}
}

//#endregion

// Default target to run when none is specified
// If not set, running mage will list available targets
//var Default = Build

// Build compiles gwcli.
// -target defaults to ./gwcli.
func Build(target *string) error {
	o := "./gwcli"
	if target != nil && *target != "" {
		o = *target
	}
	verboseln("Building " + o + "...")
	if err := build(o); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build binary '%s': %v", o, err)
		if err != nil {
			return err
		}
	}
	verboseln(good("done."))

	return nil
}

// internal build command for constructing gwcli.
// Returns where the compiled binary was placed or an error.
func build(target string) error {
	_, err := sh.Output("go", "build", "-o", target, ".")
	return err
}

// Vet runs go vet and staticcheck and should be called prior to the CI/CD pipeline.
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

	// go mod tidy
	tidyOut, tidyErr := sh.Output("go", "mod", "tidy")
	display("go mod tidy", tidyErr, tidyOut)
	// go vet
	vetOut, vetErr := sh.Output("go", "vet", "./...")
	display("go vet", vetErr, vetOut)
	// staticcheck
	scOut, scErr := sh.Output("staticcheck", "./...")
	display("staticcheck", scErr, scOut)
	return errors.Join(tidyErr, vetErr, scErr)

}

// TestAll runs all gwcli tests.
// If run with -v or an error occurs, prints outcome to stdout.
// NoCI and integration tests will be skipped if -server is not provided.
func TestAll(server *string, cover *bool) error {
	var (
		baseArgs                              = []string{"test", "-race", "-vet=all"}
		ciOut, ttOut, nociOut, integrationOut string
		ciErr, ttErr, nociErr, integrationErr error
	)
	if cover != nil && *cover {
		baseArgs = append(baseArgs, "-cover")
	}

	// validate server, if given
	if server != nil {
		if *server = strings.TrimSpace(*server); *server == "" {
			return errors.New("-server must be a valid url, likely something akin to \"localhost:80\"")
		}
	}
	// run ci tests
	fmt.Println(mid("Running CI tests..."))
	ciOut, ciErr = sh.Output("go", append(baseArgs, "-tags=ci", "./...")...)
	// run tea tests
	{
		// This has to be broken out from normal testing because golden files do not (as of 2025-07-12) play nicely with the -race flag.
		// These files should have the !race build condition, omitting them from normal processing.
		args := []string{"test", "-vet=all"}
		if cover != nil && *cover {
			args = append(args, "-cover")
		}
		args = append(args, "./tree/query/datascope")
		fmt.Println(mid("Running TeaTests..."))
		ttOut, ttErr = sh.Output("go", args...)
	}

	if server != nil { // run noci tests
		fmt.Println(mid("Running NoCI tests..."))
		// TODO pass -server in once tests actually acknowledge it
		nociOut, nociErr = sh.Output("go", append(baseArgs, "-tags=!ci", "./...")...)
		if err := Build(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to build binary: %v\n"+
				"Integration tests will be skipped.", err)
			integrationErr = errors.New("failed to build binary")
		} else {
			fmt.Println(mid("Running integration tests..."))
			integrationOut, integrationErr = sh.Output("go",
				append(baseArgs, "-tags=integration", "./integration_test.go", "-server="+*server, "--args", "./gwcli")...)
		}

	}

	// output results
	if ciErr != nil || mg.Verbose() {
		fmt.Print("CI tests ")
		if ciErr != nil {
			fmt.Println(bad("failed"))
			fmt.Println(ciOut)
		} else {
			fmt.Println(good("passed"))
		}
	}
	if ttErr != nil || mg.Verbose() {
		fmt.Print("TeaTests ")
		if ttErr != nil {
			fmt.Println(bad("failed"))
			fmt.Println(ttOut)
		} else {
			fmt.Println(good("passed"))
		}
	}
	if server != nil && (nociErr != nil || mg.Verbose()) {
		fmt.Print("NoCI tests ")
		if nociErr != nil {
			fmt.Println(bad("failed"))
			fmt.Println(nociOut)
		} else {
			fmt.Println(good("passed"))
		}

		fmt.Print("Integration tests ")
		if integrationErr != nil {
			fmt.Println(bad("failed"))
			fmt.Println(integrationOut)
		} else {
			fmt.Println(good("passed"))
		}
	}
	return nil
}

// TestIntegration calls the tests in script_test for targeting external, automated usage (via --script).
/*func TestIntegration() error {
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
	fmt.Println("NYI") // TODO

	// spit out coverage data
	out, err = sh.Output("go", "tool", "covdata", "percent", "-i="+coverdirPath)
	fmt.Println(out)
	if err != nil {
		return err
	}

	return nil
}*/

// Clean up the binary and any and all logs.
// Does not destroy login token.
//
// Running with dryrun prints out what files would be deleted, but does not actually delete them.
//
// dryrun implies -v.
//
// If an error occurs, it will immediately stop processing if !dryrun.
func Clean(dryrun bool) (err error) {
	if dryrun {
		if err := os.Setenv(mg.VerboseEnv, "1"); err != nil {
			fmt.Println("failed to imply verbose from dryrun: ", err)
		}
	}

	// destroy the binary
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
