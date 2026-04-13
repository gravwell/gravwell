/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
The build system for gwcli.
Manages testing, code generation, and (obviously) compilation.
You can use the envvar MAGEFILE_ENABLE_COLOR if you want pretty colors.
Use mage -h <target> to learn more about a given command.
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

// Build the gwcli binary.
// -target defaults to ./gwcli.
func Build(target *string) error {
	o := "./gwcli"
	if target != nil && *target != "" {
		o = *target
	}
	verboseln("Building " + o + "...")
	if err := build(o); err != nil {
		return fmt.Errorf("failed to build binary '%s': %v", o, err)
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

type Test mg.Namespace

// CI runs tests that do not require a backend to execute against.
func (Test) CI(coverage *bool) error {
	o, err := runCITests(coverage)
	printResults("CI tests", err, o)
	if err != nil {
		return errors.New("tests failed")
	}
	return nil
}

func runCITests(coverage *bool) (combinedOut string, _ error) {
	var sb strings.Builder
	verboseln(mid("Running CI tests..."))
	args := []string{"test", "-race", "-vet=all", "-tags=ci"}
	if coverage != nil && *coverage {
		args = append(args, "-cover")
	}
	args = append(args, "./...")
	_, err := sh.Exec(nil, &sb, &sb, "go", args...)
	return sb.String(), err
}

// TeaTests runs tests that rely on TeaTest and golden files.
func (Test) TeaTests(coverage *bool) error {
	o, err := runTeaTests(coverage)
	printResults("TeaTests", err, o)
	if err != nil {
		return errors.New("tests failed")
	}
	return nil
}

func runTeaTests(coverage *bool) (combinedOut string, _ error) {
	var sb strings.Builder
	// This has to be broken out from normal testing because golden files do not (as of 2025-07-12) play nicely with the -race flag.
	// These files should have the !race build condition, omitting them from normal processing.
	args := []string{"test", "-vet=all"}
	if coverage != nil && *coverage {
		args = append(args, "-cover")
	}
	args = append(args, "./tree/query/datascope")
	verboseln(mid("Running TeaTests..."))
	_, err := sh.Exec(nil, &sb, &sb, "go", args...)
	return sb.String(), err
}

// NoCI runs tests that do require a dummy gravwell instace to run against.
func (Test) NoCI(server string, coverage *bool) error {
	o, err := runNoCITests(server, coverage)
	printResults("NoCI tests", err, o)
	if err != nil {
		return errors.New("tests failed")
	}
	return nil
}

func runNoCITests(server string, coverage *bool) (combinedOut string, _ error) {
	var sb strings.Builder
	verboseln(mid("Running NoCI tests against " + server + "..."))
	args := []string{"test", "-race", "-vet=all", "-tags=noci"}
	if coverage != nil && *coverage {
		args = append(args, "-cover")
	}
	args = append(args, "./tree/query/datascope")
	_, err := sh.Exec(map[string]string{testsupport.ENV_SERVER: server}, &sb, &sb, "go", args...)
	return sb.String(), err
}

// Integration runs tests e2e tests against a compiled gwcli binary.
func (Test) Integration(server string, coverage *bool) error {
	o, err := runIntegrationTests(server, coverage)
	printResults("Integration tests", err, o)
	if err != nil {
		return errors.New("tests failed")
	}
	return nil
}

func runIntegrationTests(server string, coverage *bool) (combinedOut string, _ error) {
	var sb strings.Builder
	if err := build("./gwcli"); err != nil {
		fmt.Fprintf(&sb, "failed to build binary: %v\n", err)
		return sb.String(), errors.New("failed to build binary")
	}
	verboseln(mid("Running integration tests against " + server + "..."))
	args := []string{"test", "-race", "-vet=all", "-count=1", "-tags=integration"}
	if coverage != nil && *coverage {
		args = append(args, "-cover")
	}
	args = append(args, "./integration_noci_test.go", "-args", "-binary=./gwcli")
	_, err := sh.Exec(map[string]string{testsupport.ENV_SERVER: server}, &sb, &sb,
		"go", args...,
	)
	return sb.String(), err

}

// All runs all gwcli tests.
// If run with -v or an error occurs, prints outcome to stdout.
// NoCI and integration tests will be skipped if -server is not provided.
func (Test) All(server *string, coverage *bool) error {
	// validate server, if given
	if server != nil {
		if *server = strings.TrimSpace(*server); *server == "" {
			return errors.New("-server must be a valid url, likely akin to \"localhost:80\"")
		}
	}
	ciOut, ciErr := runCITests(coverage)
	ttOut, ttErr := runTeaTests(coverage)
	var (
		nociOut, integrationOut string
		nociErr, integrationErr error
	)
	if server != nil { // run noci tests
		nociOut, nociErr = runNoCITests(*server, coverage)
		integrationOut, integrationErr = runIntegrationTests(*server, coverage)
	}

	// output results
	printResults("CI tests", ciErr, ciOut)
	printResults("TeaTests", ttErr, ttOut)
	if server != nil {
		printResults("NoCI tests", nociErr, nociOut)
		printResults("Integration tests", integrationErr, integrationOut)
	} else {
		verboseln(strings.Repeat(" ", pad-len("NoCI tests")) + "NoCI tests " + mid("skipped"))
		verboseln(strings.Repeat(" ", pad-len("Integration tests")) + "Integration tests " + mid("skipped"))
	}
	if ciErr != nil || ttErr != nil || nociErr != nil || integrationErr != nil {
		return errors.New("some tests failed")
	}

	return nil
}

const pad int = 20

func printResults(prefix string, err error, stdout string) {
	if err != nil || mg.Verbose() {
		fmt.Print(strings.Repeat(" ", pad-len(prefix)), prefix, " ")
		if err != nil {
			fmt.Println(bad("failed"))
			fmt.Println(stdout)
		} else {
			fmt.Println(good("passed"))
		}
	}
}

// Clean up the binary and any and all logs.
// Does not destroy login token.
//
// Running with dryrun prints out what files would be deleted, but does not actually delete them.
//
// dryrun implies -v.
//
// If an error occurs, it will immediately stop processing if !dryrun.
func Clean(dryrun *bool) (err error) {
	dr := (dryrun != nil && *dryrun)

	if dr {
		oldVerbose, _ := os.LookupEnv(mg.VerboseEnv)
		if err := os.Setenv(mg.VerboseEnv, "1"); err != nil {
			fmt.Println("failed to imply verbose from dryrun: ", err)
		} else {
			defer os.Setenv(mg.VerboseEnv, oldVerbose)
		}
	}

	// destroy the binary
	binPath := path.Join(".", _BINARY_TARGET)
	if err := dryRM(binPath, dr); err != nil {
		return err
	}

	// Destroy log files in the config directory
	if err := dryRM(cfgdir.DefaultStdLogPath, dr); err != nil {
		return err
	}
	if err := dryRM(cfgdir.DefaultRestLogPath, dr); err != nil {
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
